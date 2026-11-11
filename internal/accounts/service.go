// Package accounts owns trusted endpoint registration and encrypted credentials.
package accounts

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode/utf8"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/internal/vault"
)

type InputError struct{ Message string }

func (e *InputError) Error() string { return e.Message }

var ErrEndpoint = errors.New("接入点尚未完成独立核验和风险确认")
var ErrTarget = errors.New("目标不是允许的公开接入地址")
var ErrPaused = errors.New("账户采集已暂停")

type Service struct {
	Store       *storage.Store
	Vault       *vault.Vault
	AllowTarget TargetPolicy
	Now         func() time.Time
}

func New(store *storage.Store, key *vault.Vault, policy TargetPolicy) *Service {
	if policy == nil {
		policy = PublicTarget
	}
	return &Service{Store: store, Vault: key, AllowTarget: policy, Now: time.Now}
}

type ServerInput struct {
	BrokerName string `json:"broker_name"`
	ServerName string `json:"server_name"`
	IPAddress  string `json:"ip_address"`
	Port       int    `json:"port"`
	TimeZone   string `json:"time_zone"`
}

type EndpointInput struct {
	IPAddress string `json:"ip_address"`
	Port      int    `json:"port"`
}

type ReviewInput struct {
	EvidenceRef    string `json:"evidence_ref"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	AcceptRisk     bool   `json:"accept_risk"`
}

type AccountInput struct {
	ServerID          string `json:"server_id"`
	EndpointID        string `json:"endpoint_id"`
	Login             uint32 `json:"login"`
	Label             string `json:"label"`
	Password          string `json:"password"`
	ConfirmedCurrency string `json:"confirmed_currency"`
	ConfirmUnit       bool   `json:"confirm_unit"`
	SyncEnabled       bool   `json:"sync_enabled"`
}

type AccountPatch struct {
	ConfigVersion     *int64  `json:"config_version"`
	Label             *string `json:"label"`
	EndpointID        *string `json:"endpoint_id"`
	Password          *string `json:"password"`
	ConfirmedCurrency *string `json:"confirmed_currency"`
	ConfirmUnit       *bool   `json:"confirm_unit"`
	SyncEnabled       *bool   `json:"sync_enabled"`
}

func (s *Service) CreateServer(ctx context.Context, input ServerInput) (storage.Server, storage.Endpoint, error) {
	if !clean(input.BrokerName, 1, 128) || !clean(input.ServerName, 1, 128) {
		return storage.Server{}, storage.Endpoint{}, &InputError{"请填写经纪商和准确的服务器名称"}
	}
	if !s.AllowTarget(input.IPAddress) || input.Port < 1 || input.Port > 65535 {
		return storage.Server{}, storage.Endpoint{}, ErrTarget
	}
	if input.TimeZone != "" {
		if _, err := time.LoadLocation(input.TimeZone); err != nil {
			return storage.Server{}, storage.Endpoint{}, &InputError{"时区名称无效"}
		}
	}
	now := s.Now().UnixMilli()
	server := storage.Server{ID: storage.NewID(), Protocol: "mt4", BrokerName: strings.TrimSpace(input.BrokerName), ServerName: strings.TrimSpace(input.ServerName), CreatedAtMS: now}
	endpoint := storage.Endpoint{ID: storage.NewID(), ServerID: server.ID, Revision: 1, IPAddress: input.IPAddress, Port: input.Port, State: "pending", CreatedAtMS: now}
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := tx.Create(&server).Error; err != nil {
			return err
		}
		if err := tx.Create(&endpoint).Error; err != nil {
			return err
		}
		if input.TimeZone != "" {
			rules, _ := json.Marshal([]map[string]any{{"from_utc_ms": now, "until_utc_ms": nil, "kind": "iana", "zone": input.TimeZone, "source_ref": "用户登记，尚未核验"}})
			return tx.Exec("INSERT INTO server_time_rule_sets (id, server_id, revision, rules_json, created_at_ms) VALUES (?, ?, 1, ?, ?)", storage.NewID(), server.ID, string(rules), now).Error
		}
		return nil
	})
	return server, endpoint, err
}

func (s *Service) NewEndpoint(ctx context.Context, serverID string, input EndpointInput) (storage.Endpoint, []string, error) {
	if !s.AllowTarget(input.IPAddress) || input.Port < 1 || input.Port > 65535 {
		return storage.Endpoint{}, nil, ErrTarget
	}
	endpoint := storage.Endpoint{ID: storage.NewID(), ServerID: serverID, IPAddress: input.IPAddress, Port: input.Port, State: "pending", CreatedAtMS: s.Now().UnixMilli()}
	var paused []string
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var server storage.Server
		if err := tx.First(&server, "id = ? AND retired_at_ms IS NULL", serverID).Error; err != nil {
			return err
		}
		var revision int64
		if err := tx.Model(&storage.Endpoint{}).Where("server_id = ?", serverID).Select("COALESCE(MAX(revision), 0)").Scan(&revision).Error; err != nil {
			return err
		}
		endpoint.Revision = revision + 1
		if err := tx.Model(&storage.Endpoint{}).Where("server_id = ? AND state != 'retired'", serverID).Update("state", "retired").Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.Account{}).Where("server_id = ?", serverID).Pluck("id", &paused).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.Account{}).Where("server_id = ?", serverID).Updates(map[string]any{"sync_enabled": false, "config_version": gorm.Expr("config_version + 1")}).Error; err != nil {
			return err
		}
		return tx.Create(&endpoint).Error
	})
	return endpoint, paused, err
}

func (s *Service) Review(ctx context.Context, id string, input ReviewInput) (storage.Endpoint, error) {
	digest, err := hex.DecodeString(input.EvidenceSHA256)
	if err != nil || len(digest) != 32 || !clean(input.EvidenceRef, 1, 512) {
		return storage.Endpoint{}, &InputError{"请填写独立核验依据和64位十六进制证据摘要"}
	}
	var endpoint storage.Endpoint
	err = s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := tx.First(&endpoint, "id = ?", id).Error; err != nil {
			return err
		}
		if endpoint.State == "retired" || !s.AllowTarget(endpoint.IPAddress) {
			return ErrEndpoint
		}
		if endpoint.State == "reviewed" && (endpoint.EvidenceRef == nil || *endpoint.EvidenceRef != input.EvidenceRef || !bytes.Equal(endpoint.EvidenceSHA256, digest)) {
			return &InputError{"核验来源变化需要建立新的接入点修订"}
		}
		now := s.Now().UnixMilli()
		endpoint.State, endpoint.EvidenceRef, endpoint.EvidenceSHA256 = "reviewed", &input.EvidenceRef, digest
		if endpoint.ReviewedAtMS == nil {
			endpoint.ReviewedAtMS = &now
		}
		if input.AcceptRisk && endpoint.RiskAcceptedAtMS == nil {
			endpoint.RiskAcceptedAtMS = &now
		}
		return tx.Model(&storage.Endpoint{}).Where("id = ?", id).Updates(map[string]any{
			"state": endpoint.State, "evidence_ref": endpoint.EvidenceRef, "evidence_sha256": digest,
			"reviewed_at_ms": endpoint.ReviewedAtMS, "risk_accepted_at_ms": endpoint.RiskAcceptedAtMS,
		}).Error
	})
	return endpoint, err
}

func checkPassword(password string) error {
	if !utf8.ValidString(password) || len(password) < 1 || len(password) > 16 || strings.ContainsRune(password, 0) {
		return &InputError{"投资者密码需要1至16个UTF-8字节"}
	}
	return nil
}

func setUnit(account *storage.Account, currency string, confirmed bool, now int64) error {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency != "" && !clean(currency, 1, 12) {
		return &InputError{"账户币种无效"}
	}
	account.Currency, account.USDScale, account.UnitVerifiedAtMS = nil, nil, nil
	if currency == "" {
		if confirmed {
			return &InputError{"请先填写确认的账户币种"}
		}
		return nil
	}
	account.Currency = &currency
	if confirmed {
		account.UnitVerifiedAtMS = &now
		if currency == "USD" {
			value := decimal.NewFromInt(1)
			account.USDScale = &value
		} else if currency == "USC" {
			value := decimal.RequireFromString("0.01")
			account.USDScale = &value
		}
	}
	return nil
}

func (s *Service) validEndpoint(tx *gorm.DB, id, server string) (storage.Endpoint, error) {
	var endpoint storage.Endpoint
	if err := tx.First(&endpoint, "id = ? AND server_id = ?", id, server).Error; err != nil {
		return endpoint, err
	}
	if !endpoint.Approved() {
		return endpoint, ErrEndpoint
	}
	if !s.AllowTarget(endpoint.IPAddress) {
		return endpoint, ErrTarget
	}
	return endpoint, nil
}

func (s *Service) CreateAccount(ctx context.Context, input AccountInput) (storage.Account, error) {
	if !s.Vault.Ready() {
		return storage.Account{}, vault.ErrUnavailable
	}
	if input.Login == 0 || !clean(input.Label, 1, 96) {
		return storage.Account{}, &InputError{"请填写账户名称和有效的MT4账号"}
	}
	if err := checkPassword(input.Password); err != nil {
		return storage.Account{}, err
	}
	now := s.Now().UnixMilli()
	account := storage.Account{ID: storage.NewID(), ServerID: input.ServerID, PinnedEndpointID: input.EndpointID, Login: input.Login,
		Label: strings.TrimSpace(input.Label), CredentialVersion: 1, ConfigVersion: 1, UnitRevision: 1,
		SyncEnabled: input.SyncEnabled, NextSyncAtMS: now, CreatedAtMS: now, CredentialKeyID: s.Vault.ID()}
	if err := setUnit(&account, input.ConfirmedCurrency, input.ConfirmUnit, now); err != nil {
		return account, err
	}
	var err error
	account.CredentialEnvelope, err = s.Vault.Seal(input.Password, vault.AAD(account.ID, account.ServerID, account.PinnedEndpointID, 1))
	if err != nil {
		return account, err
	}
	err = s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if _, err := s.validEndpoint(tx, input.EndpointID, input.ServerID); err != nil {
			return err
		}
		return tx.Create(&account).Error
	})
	return account, err
}

func (s *Service) UpdateAccount(ctx context.Context, id string, patch AccountPatch) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id = ?", id).Error; err != nil {
			return err
		}
		if patch.ConfigVersion != nil && account.ConfigVersion != *patch.ConfigVersion {
			return storage.ErrConflict
		}
		if patch.Label != nil {
			if !clean(*patch.Label, 1, 96) {
				return &InputError{"账户名称无效"}
			}
			account.Label = strings.TrimSpace(*patch.Label)
		}
		oldEndpoint := account.PinnedEndpointID
		if patch.EndpointID != nil {
			account.PinnedEndpointID = *patch.EndpointID
		}
		if patch.Password != nil || account.PinnedEndpointID != oldEndpoint {
			if !s.Vault.Ready() {
				return vault.ErrUnavailable
			}
			var password string
			if patch.Password != nil {
				password = *patch.Password
			} else {
				var err error
				password, err = s.Vault.Open(account.CredentialEnvelope, account.CredentialKeyID, vault.AAD(account.ID, account.ServerID, oldEndpoint, account.CredentialVersion))
				if err != nil {
					return err
				}
			}
			if err := checkPassword(password); err != nil {
				return err
			}
			account.CredentialVersion++
			var err error
			account.CredentialEnvelope, err = s.Vault.Seal(password, vault.AAD(account.ID, account.ServerID, account.PinnedEndpointID, account.CredentialVersion))
			if err != nil {
				return err
			}
			account.CredentialKeyID = s.Vault.ID()
		}
		now := s.Now().UnixMilli()
		if patch.ConfirmedCurrency != nil || patch.ConfirmUnit != nil {
			currency := ""
			if account.Currency != nil {
				currency = *account.Currency
			}
			if patch.ConfirmedCurrency != nil {
				currency = *patch.ConfirmedCurrency
			}
			confirmed := account.UnitVerifiedAtMS != nil
			if patch.ConfirmUnit != nil {
				confirmed = *patch.ConfirmUnit
			}
			previousCurrency := ""
			if account.Currency != nil {
				previousCurrency = *account.Currency
			}
			if strings.ToUpper(strings.TrimSpace(currency)) != previousCurrency || confirmed != (account.UnitVerifiedAtMS != nil) {
				account.UnitRevision++
				if err := setUnit(&account, currency, confirmed, now); err != nil {
					return err
				}
			}
		}
		if patch.SyncEnabled != nil {
			account.SyncEnabled = *patch.SyncEnabled
		}
		if account.SyncEnabled || account.PinnedEndpointID != oldEndpoint {
			if _, err := s.validEndpoint(tx, account.PinnedEndpointID, account.ServerID); err != nil {
				return err
			}
		}
		account.ConfigVersion++
		return tx.Model(&storage.Account{}).Where("id = ?", id).Updates(map[string]any{
			"label": account.Label, "pinned_endpoint_id": account.PinnedEndpointID, "credential_envelope": account.CredentialEnvelope,
			"credential_key_id": account.CredentialKeyID, "credential_version": account.CredentialVersion, "config_version": account.ConfigVersion,
			"currency": account.Currency, "usd_scale": account.USDScale, "unit_revision": account.UnitRevision, "unit_verified_at_ms": account.UnitVerifiedAtMS,
			"sync_enabled": account.SyncEnabled, "next_sync_at_ms": now, "failure_count": 0,
		}).Error
	})
}
