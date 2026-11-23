package accounts

import (
	"encoding/json"
	"errors"
	"net"
	"net/netip"
	"slices"
	"strconv"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/internal/vault"
)

const maxBackupEndpoints = 2

func backupIDs(account storage.Account) []string {
	ids := []string{}
	// SQLite enforces valid JSON. The credential envelope authenticates the
	// exact stored value, so a changed list cannot grant credential access.
	_ = json.Unmarshal([]byte(account.BackupEndpointIDs), &ids)
	return ids
}

func credentialAAD(account storage.Account) []byte {
	aad := vault.AAD(account.ID, account.ServerID, account.PinnedEndpointID, account.CredentialVersion)
	if account.BackupEndpointIDs != "" && account.BackupEndpointIDs != "[]" {
		aad = append(aad, '\x00')
		aad = append(aad, account.BackupEndpointIDs...)
	}
	return aad
}

func collectionEndpointID(account storage.Account) string {
	if account.LastCollectionEndpointID != "" {
		return account.LastCollectionEndpointID
	}
	return account.PinnedEndpointID
}

func (s *Service) setBackupEndpoints(tx *gorm.DB, account *storage.Account, ids []string) error {
	if len(ids) > maxBackupEndpoints {
		return &InputError{"最多可绑定两个备用采集点"}
	}
	addresses := map[string]bool{}
	for _, id := range append([]string{account.PinnedEndpointID}, ids...) {
		endpoint, err := s.validEndpoint(tx, id, account.ServerID)
		if err != nil {
			return err
		}
		address := endpointAddress(endpoint)
		if addresses[address] {
			return &InputError{"采集点地址不能重复"}
		}
		addresses[address] = true
	}
	encoded, err := json.Marshal(append([]string{}, ids...))
	if err != nil {
		return err
	}
	account.BackupEndpointIDs = string(encoded)
	return nil
}

func endpointAddress(endpoint storage.Endpoint) string {
	ip, err := netip.ParseAddr(endpoint.IPAddress)
	if err == nil {
		return net.JoinHostPort(ip.Unmap().String(), strconv.Itoa(endpoint.Port))
	}
	return net.JoinHostPort(endpoint.IPAddress, strconv.Itoa(endpoint.Port))
}

func (s *Service) collectionEndpoints(tx *gorm.DB, account storage.Account) ([]storage.Endpoint, error) {
	ids := append([]string{account.PinnedEndpointID}, backupIDs(account)...)
	if i := slices.Index(ids, account.LastCollectionEndpointID); i > 0 {
		last := ids[i]
		copy(ids[1:i+1], ids[:i])
		ids[0] = last
	}
	var available []storage.Endpoint
	var unavailable error = ErrEndpoint
	for _, id := range ids {
		endpoint, err := s.validEndpoint(tx, id, account.ServerID)
		switch {
		case err == nil:
			available = append(available, endpoint)
		case errors.Is(err, ErrEndpoint), errors.Is(err, ErrTarget), errors.Is(err, storage.ErrNotFound):
			if id == account.PinnedEndpointID {
				unavailable = ErrEndpoint
				if errors.Is(err, ErrTarget) {
					unavailable = err
				}
			}
		default:
			return nil, err
		}
	}
	if len(available) == 0 {
		return nil, unavailable
	}
	return available, nil
}
