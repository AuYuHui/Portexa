package accounts

import (
	"context"
	"net"
	"slices"
	"strconv"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/mt4"
	"portexa/mt5"
)

type collectionConfig struct {
	CollectionTarget
	backups []string
}

func (s *Service) collectionConfig(tx *gorm.DB, account storage.Account) (collectionConfig, error) {
	endpoints, err := s.collectionEndpoints(tx, account)
	if err != nil {
		return collectionConfig{}, err
	}
	target, err := s.targetConfig(tx, account, endpoints[0])
	cfg := collectionConfig{CollectionTarget: target}
	for _, endpoint := range endpoints[1:] {
		cfg.backups = append(cfg.backups, endpoint.ID)
	}
	return cfg, err
}

// CollectionTarget is an internal, endpoint-specific credential capability.
type CollectionTarget struct {
	ID  string
	MT4 mt4.Config
	MT5 *mt5.Config
}

func (s *Service) targetConfig(tx *gorm.DB, account storage.Account, endpoint storage.Endpoint) (CollectionTarget, error) {
	var server storage.Server
	if err := tx.First(&server, "id = ? AND retired_at_ms IS NULL", account.ServerID).Error; err != nil {
		return CollectionTarget{}, err
	}
	password, err := s.Vault.Open(account.CredentialEnvelope, account.CredentialKeyID, credentialAAD(account))
	if err != nil {
		return CollectionTarget{}, err
	}
	if server.Protocol == "mt5" {
		return CollectionTarget{ID: endpoint.ID, MT5: &mt5.Config{IPAddress: endpoint.IPAddress, Port: endpoint.Port,
			Server: server.ServerName, Login: account.Login, Password: password}}, nil
	}
	address := net.JoinHostPort(endpoint.IPAddress, strconv.Itoa(endpoint.Port))
	return CollectionTarget{ID: endpoint.ID, MT4: mt4.Config{Address: address, Server: server.ServerName,
		Login: uint32(account.Login), Password: password, PasswordType: mt4.InvestorPassword}}, nil
}

func (s *Service) fetchConfig(tx *gorm.DB, account storage.Account) (mt4.Config, error) {
	id := collectionEndpointID(account)
	if id != account.PinnedEndpointID && !slices.Contains(backupIDs(account), id) {
		return mt4.Config{}, ErrEndpoint
	}
	endpoint, err := s.validEndpoint(tx, id, account.ServerID)
	if err != nil {
		return mt4.Config{}, err
	}
	cfg, err := s.targetConfig(tx, account, endpoint)
	if cfg.MT5 != nil {
		return mt4.Config{}, ErrPaused
	}
	return cfg.MT4, err
}

// SwitchCollectionTarget verifies the live configuration again and records
// the actual endpoint before issuing a fresh credential capability.
func (s *Service) SwitchCollectionTarget(ctx context.Context, previous storage.Account, run any, runID, endpointID string) (CollectionTarget, error) {
	var target CollectionTarget
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var current storage.Account
		if err := tx.First(&current, "id = ?", previous.ID).Error; err != nil {
			return err
		}
		if !current.SyncEnabled || current.RemovedAtMS != nil || current.ConfigVersion != previous.ConfigVersion ||
			(endpointID != current.PinnedEndpointID && !slices.Contains(backupIDs(current), endpointID)) {
			return storage.ErrConflict
		}
		endpoint, err := s.validEndpoint(tx, endpointID, current.ServerID)
		if err != nil {
			return err
		}
		target, err = s.targetConfig(tx, current, endpoint)
		if err != nil {
			return err
		}
		result := tx.Model(run).Where("id = ? AND account_id = ? AND config_version = ? AND state = 'running'",
			runID, current.ID, current.ConfigVersion).Update("endpoint_id", endpointID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return storage.ErrConflict
		}
		return nil
	})
	return target, err
}
