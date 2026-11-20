package accounts

import (
	"net"
	"strconv"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/internal/vault"
	"portexa/mt4"
	"portexa/mt5"
)

type collectionConfig struct {
	mt4 mt4.Config
	mt5 *mt5.Config
}

func (s *Service) collectionConfig(tx *gorm.DB, account storage.Account) (collectionConfig, error) {
	endpoint, err := s.validEndpoint(tx, account.PinnedEndpointID, account.ServerID)
	if err != nil {
		return collectionConfig{}, err
	}
	var server storage.Server
	if err = tx.First(&server, "id = ? AND retired_at_ms IS NULL", account.ServerID).Error; err != nil {
		return collectionConfig{}, err
	}
	password, err := s.Vault.Open(account.CredentialEnvelope, account.CredentialKeyID,
		vault.AAD(account.ID, account.ServerID, endpoint.ID, account.CredentialVersion))
	if err != nil {
		return collectionConfig{}, err
	}
	if server.Protocol == "mt5" {
		return collectionConfig{mt5: &mt5.Config{IPAddress: endpoint.IPAddress, Port: endpoint.Port,
			Server: server.ServerName, Login: account.Login, Password: password}}, nil
	}
	return collectionConfig{mt4: mt4.Config{Address: net.JoinHostPort(endpoint.IPAddress, strconv.Itoa(endpoint.Port)), Server: server.ServerName,
		Login: uint32(account.Login), Password: password, PasswordType: mt4.InvestorPassword}}, nil
}

func (s *Service) fetchConfig(tx *gorm.DB, account storage.Account) (mt4.Config, error) {
	cfg, err := s.collectionConfig(tx, account)
	if cfg.mt5 != nil {
		return mt4.Config{}, ErrPaused
	}
	return cfg.mt4, err
}
