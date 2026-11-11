package accounts

import (
	"net"
	"strconv"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/internal/vault"
	"portexa/mt4"
)

func (s *Service) fetchConfig(tx *gorm.DB, account storage.Account) (mt4.Config, error) {
	endpoint, err := s.validEndpoint(tx, account.PinnedEndpointID, account.ServerID)
	if err != nil {
		return mt4.Config{}, err
	}
	var server storage.Server
	if err = tx.First(&server, "id = ? AND retired_at_ms IS NULL", account.ServerID).Error; err != nil {
		return mt4.Config{}, err
	}
	password, err := s.Vault.Open(account.CredentialEnvelope, account.CredentialKeyID,
		vault.AAD(account.ID, account.ServerID, endpoint.ID, account.CredentialVersion))
	if err != nil {
		return mt4.Config{}, err
	}
	return mt4.Config{Address: net.JoinHostPort(endpoint.IPAddress, strconv.Itoa(endpoint.Port)), Server: server.ServerName,
		Login: account.Login, Password: password, PasswordType: mt4.InvestorPassword}, nil
}
