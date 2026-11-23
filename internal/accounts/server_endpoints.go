package accounts

import (
	"context"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

func (s *Service) buildEndpoint(tx *gorm.DB, serverID string, input EndpointInput, revision int64, approval *endpointApproval, now int64) (storage.Endpoint, error) {
	if !s.AllowTarget(input.IPAddress) || input.Port < 1 || input.Port > 65535 {
		return storage.Endpoint{}, ErrTarget
	}
	endpoint := storage.Endpoint{ID: storage.NewID(), ServerID: serverID, Revision: revision, IPAddress: input.IPAddress,
		Port: input.Port, State: "pending", CreatedAtMS: now}
	err := approval.apply(tx, &endpoint, now)
	return endpoint, err
}

func (s *Service) addEndpoints(ctx context.Context, serverID string, inputs []EndpointInput, approval *endpointApproval) ([]storage.Endpoint, error) {
	var added []storage.Endpoint
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var server storage.Server
		if err := tx.First(&server, "id = ? AND retired_at_ms IS NULL", serverID).Error; err != nil {
			return err
		}
		if !approval.matchesServer(server) {
			return &InputError{"新接入点必须属于当前服务器及交易平台"}
		}
		var existing []storage.Endpoint
		if err := tx.Where("server_id = ?", serverID).Order("revision").Find(&existing).Error; err != nil {
			return err
		}
		addresses := make(map[string]storage.Endpoint)
		var revision int64
		for _, endpoint := range existing {
			revision = endpoint.Revision
			if endpoint.State != "retired" {
				addresses[endpointAddress(endpoint)] = endpoint
			}
		}
		now := s.Now().UnixMilli()
		for _, input := range inputs {
			if !s.AllowTarget(input.IPAddress) || input.Port < 1 || input.Port > 65535 {
				return ErrTarget
			}
			address := endpointAddress(storage.Endpoint{IPAddress: input.IPAddress, Port: input.Port})
			if endpoint, ok := addresses[address]; ok {
				if approval != nil && !endpoint.Approved() {
					return &InputError{"采集点已登记，请先完成该接入点的核验和风险确认"}
				}
				added = append(added, endpoint)
				continue
			}
			endpoint, err := s.buildEndpoint(tx, serverID, input, revision+1, approval, now)
			if err != nil {
				return err
			}
			if err = tx.Create(&endpoint).Error; err != nil {
				return err
			}
			revision++
			addresses[address] = endpoint
			added = append(added, endpoint)
		}
		return nil
	})
	return added, err
}
