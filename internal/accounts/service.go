// Package accounts owns trusted endpoint registration and encrypted credentials.
package accounts

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
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
	Protocol   string `json:"protocol"`
	BrokerName string `json:"broker_name"`
	ServerName string `json:"server_name"`
	IPAddress  string `json:"ip_address"`
	Port       int    `json:"port"`
	TimeZone   string `json:"time_zone"`
}

type EndpointInput struct {
	IPAddress    string `json:"ip_address"`
	Port         int    `json:"port"`
	KeepExisting bool   `json:"keep_existing"`
}

type ReviewInput struct {
	EvidenceRef    string `json:"evidence_ref"`
	EvidenceSHA256 string `json:"evidence_sha256"`
	AcceptRisk     bool   `json:"accept_risk"`
}

type AccountInput struct {
	ServerID          string   `json:"server_id"`
	EndpointID        string   `json:"endpoint_id"`
	BackupEndpointIDs []string `json:"backup_endpoint_ids"`
	Login             uint64   `json:"login"`
	Label             string   `json:"label"`
	Password          string   `json:"password"`
	ConfirmedCurrency string   `json:"confirmed_currency"`
	ConfirmUnit       bool     `json:"confirm_unit"`
	SyncEnabled       bool     `json:"sync_enabled"`
}

type AccountPatch struct {
	ConfigVersion     *int64    `json:"config_version"`
	Label             *string   `json:"label"`
	EndpointID        *string   `json:"endpoint_id"`
	BackupEndpointIDs *[]string `json:"backup_endpoint_ids"`
	Password          *string   `json:"password"`
	ConfirmedCurrency *string   `json:"confirmed_currency"`
	ConfirmUnit       *bool     `json:"confirm_unit"`
	SyncEnabled       *bool     `json:"sync_enabled"`
}

func (s *Service) CreateServer(ctx context.Context, input ServerInput) (storage.Server, storage.Endpoint, error) {
	return s.createServer(ctx, input, nil)
}

func (s *Service) createServer(ctx context.Context, input ServerInput, approval *endpointApproval, additional ...EndpointInput) (storage.Server, storage.Endpoint, error) {
	if !clean(input.BrokerName, 1, 128) || !clean(input.ServerName, 1, 128) {
		return storage.Server{}, storage.Endpoint{}, &InputError{"请填写经纪商和准确的服务器名称"}
	}
	if !s.AllowTarget(input.IPAddress) || input.Port < 1 || input.Port > 65535 {
		return storage.Server{}, storage.Endpoint{}, ErrTarget
	}
	protocol := input.Protocol
	if protocol == "" {
		protocol = "mt4"
	}
	if protocol != "mt4" && protocol != "mt5" {
		return storage.Server{}, storage.Endpoint{}, &InputError{"请选择 MT4 或 MT5"}
	}
	if input.TimeZone != "" {
		if _, err := time.LoadLocation(input.TimeZone); err != nil {
			return storage.Server{}, storage.Endpoint{}, &InputError{"时区名称无效"}
		}
	}
	now := s.Now().UnixMilli()
	server := storage.Server{ID: storage.NewID(), Protocol: protocol, BrokerName: strings.TrimSpace(input.BrokerName), ServerName: strings.TrimSpace(input.ServerName), CreatedAtMS: now}
	endpoint := storage.Endpoint{ID: storage.NewID(), ServerID: server.ID, Revision: 1, IPAddress: input.IPAddress, Port: input.Port, State: "pending", CreatedAtMS: now}
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := approval.apply(tx, &endpoint, now); err != nil {
			return err
		}
		if err := tx.Create(&server).Error; err != nil {
			return err
		}
		if err := tx.Create(&endpoint).Error; err != nil {
			return err
		}
		for i, candidate := range additional {
			backup, err := s.buildEndpoint(tx, server.ID, candidate, int64(i+2), approval, now)
			if err != nil {
				return err
			}
			if err = tx.Create(&backup).Error; err != nil {
				return err
			}
		}
		if input.TimeZone != "" {
			rules, _ := json.Marshal([]map[string]any{{"from_utc_ms": now, "until_utc_ms": nil, "kind": "iana", "zone": input.TimeZone, "source_ref": "用户登记，尚未核验"}})
			if err := tx.Exec("INSERT INTO server_time_rule_sets (id, server_id, revision, rules_json, created_at_ms) VALUES (?, ?, 1, ?, ?)", storage.NewID(), server.ID, string(rules), now).Error; err != nil {
				return err
			}
		}
		return ensureAutomaticTime(tx, &server, s.Now())
	})
	return server, endpoint, err
}

func (s *Service) NewEndpoint(ctx context.Context, serverID string, input EndpointInput) (storage.Endpoint, []string, error) {
	if input.KeepExisting {
		endpoints, err := s.addEndpoints(ctx, serverID, []EndpointInput{input}, nil)
		if err != nil {
			return storage.Endpoint{}, nil, err
		}
		return endpoints[0], nil, nil
	}
	return s.newEndpoint(ctx, serverID, input, nil)
}

func (s *Service) newEndpoint(ctx context.Context, serverID string, input EndpointInput, approval *endpointApproval) (storage.Endpoint, []string, error) {
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
		if !approval.matchesServer(server) {
			return &InputError{"新接入点必须属于当前服务器及交易平台"}
		}
		if err := approval.apply(tx, &endpoint, s.Now().UnixMilli()); err != nil {
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
		if err := tx.Model(&storage.Account{}).Where("server_id = ? AND removed_at_ms IS NULL", serverID).Pluck("id", &paused).Error; err != nil {
			return err
		}
		if err := tx.Model(&storage.Account{}).Where("server_id = ? AND removed_at_ms IS NULL", serverID).Updates(map[string]any{"sync_enabled": false, "config_version": gorm.Expr("config_version + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Create(&endpoint).Error; err != nil {
			return err
		}
		return refreshClockCalibration(tx, &server, s.Now())
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
	if input.Login == 0 || input.Login > maxBrowserInteger || !clean(input.Label, 1, 96) {
		return storage.Account{}, &InputError{"请填写账户名称和有效的交易账号"}
	}
	now := s.Now().UnixMilli()
	account := storage.Account{ID: storage.NewID(), ServerID: input.ServerID, PinnedEndpointID: input.EndpointID, Login: input.Login,
		Label: strings.TrimSpace(input.Label), CredentialVersion: 1, ConfigVersion: 1, UnitRevision: 1,
		SyncEnabled: input.SyncEnabled, NextSyncAtMS: now, CreatedAtMS: now, CredentialKeyID: s.Vault.ID()}
	if err := setUnit(&account, input.ConfirmedCurrency, input.ConfirmUnit, now); err != nil {
		return account, err
	}
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		if err := s.setBackupEndpoints(tx, &account, input.BackupEndpointIDs); err != nil {
			return err
		}
		var server storage.Server
		if err := tx.First(&server, "id = ? AND retired_at_ms IS NULL", input.ServerID).Error; err != nil {
			return err
		}
		if server.Protocol == "mt4" && input.Login > 4294967295 {
			return &InputError{"请填写账户名称和有效的交易账号"}
		}
		if err := checkProtocolPassword(server.Protocol, input.Password); err != nil {
			return err
		}
		envelope, err := s.Vault.Seal(input.Password, credentialAAD(account))
		if err != nil {
			return err
		}
		account.CredentialEnvelope = envelope
		if err := tx.Create(&account).Error; err != nil {
			return err
		}
		return tx.Create(&storage.AccountUnitChange{AccountID: account.ID, UnitRevision: account.UnitRevision, EffectiveAtMS: now}).Error
	})
	return account, err
}

func (s *Service) UpdateAccount(ctx context.Context, id string, patch AccountPatch) error {
	return s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
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
		var server storage.Server
		if err := tx.First(&server, "id = ?", account.ServerID).Error; err != nil {
			return err
		}
		oldAAD := credentialAAD(account)
		oldEndpoint, oldBackups := account.PinnedEndpointID, account.BackupEndpointIDs
		oldUnitRevision, wasEnabled := account.UnitRevision, account.SyncEnabled
		if patch.EndpointID != nil {
			account.PinnedEndpointID = *patch.EndpointID
		}
		if patch.BackupEndpointIDs != nil || account.PinnedEndpointID != oldEndpoint {
			ids := backupIDs(account)
			if patch.BackupEndpointIDs != nil {
				ids = *patch.BackupEndpointIDs
			} else {
				ids = slices.DeleteFunc(ids, func(id string) bool { return id == account.PinnedEndpointID })
			}
			if err := s.setBackupEndpoints(tx, &account, ids); err != nil {
				return err
			}
		}
		endpointsChanged := account.PinnedEndpointID != oldEndpoint || account.BackupEndpointIDs != oldBackups
		if patch.Password != nil || endpointsChanged {
			if !s.Vault.Ready() {
				return vault.ErrUnavailable
			}
			var password string
			if patch.Password != nil {
				password = *patch.Password
			} else {
				var err error
				password, err = s.Vault.Open(account.CredentialEnvelope, account.CredentialKeyID, oldAAD)
				if err != nil {
					return err
				}
			}
			if err := checkProtocolPassword(server.Protocol, password); err != nil {
				return err
			}
			account.CredentialVersion++
			var err error
			account.CredentialEnvelope, err = s.Vault.Seal(password, credentialAAD(account))
			if err != nil {
				return err
			}
			account.CredentialKeyID = s.Vault.ID()
			account.LastCollectionEndpointID = ""
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
		if account.SyncEnabled {
			if _, err := s.collectionEndpoints(tx, account); err != nil {
				return err
			}
		}
		account.ConfigVersion++
		if account.UnitRevision != oldUnitRevision {
			if err := tx.Create(&storage.AccountUnitChange{AccountID: id, UnitRevision: account.UnitRevision, EffectiveAtMS: now}).Error; err != nil {
				return err
			}
		}
		dailyPaused, dailyFailures := account.DailySyncPaused, account.DailyFailureCount
		clockPaused, clockFailures := account.ClockSyncPaused, account.ClockFailureCount
		historyPaused := account.HistorySyncPaused
		if patch.Password != nil || endpointsChanged || account.UnitRevision != oldUnitRevision || (!wasEnabled && account.SyncEnabled) {
			dailyPaused, dailyFailures = false, 0
			clockPaused, clockFailures = false, 0
			historyPaused = false
		}
		if err := tx.Model(&storage.Account{}).Where("id = ?", id).Updates(map[string]any{
			"label": account.Label, "pinned_endpoint_id": account.PinnedEndpointID, "credential_envelope": account.CredentialEnvelope,
			"backup_endpoint_ids": account.BackupEndpointIDs, "last_collection_endpoint_id": account.LastCollectionEndpointID,
			"credential_key_id": account.CredentialKeyID, "credential_version": account.CredentialVersion, "config_version": account.ConfigVersion,
			"currency": account.Currency, "usd_scale": account.USDScale, "unit_revision": account.UnitRevision, "unit_verified_at_ms": account.UnitVerifiedAtMS,
			"sync_enabled": account.SyncEnabled, "next_sync_at_ms": now, "failure_count": 0,
			"next_daily_sync_at_ms": 0, "daily_failure_count": dailyFailures, "daily_sync_paused": dailyPaused,
			"next_clock_sync_at_ms": 0, "clock_failure_count": clockFailures, "clock_sync_paused": clockPaused,
			"next_history_sync_at_ms": 0, "history_sync_paused": historyPaused,
		}).Error; err != nil {
			return err
		}
		return refreshClockCalibration(tx, &server, s.Now())
	})
}
