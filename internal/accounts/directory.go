package accounts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/discovery"
	"portexa/internal/storage"
)

type endpointApproval struct {
	ref    string
	digest []byte
	source discovery.Candidate
}

// CreateDiscovered uses the server-held candidate, never a client-supplied IP,
// company, URL or digest. User risk acceptance is explicit for this revision.
func (s *Service) CreateDiscovered(ctx context.Context, candidate discovery.Candidate, index int, acceptRisk bool) (storage.Server, storage.Endpoint, error) {
	return s.CreateDiscoveredServer(ctx, candidate, []int{index}, acceptRisk)
}

func (s *Service) NewDiscoveredEndpoint(ctx context.Context, serverID string, candidate discovery.Candidate, index int, acceptRisk bool) (storage.Endpoint, []string, error) {
	approval, access, err := s.approveCandidate(candidate, index, acceptRisk)
	if err != nil {
		return storage.Endpoint{}, nil, err
	}
	return s.newEndpoint(ctx, serverID, EndpointInput{IPAddress: access.IPAddress, Port: access.Port}, approval)
}

func (s *Service) CreateDiscoveredServer(ctx context.Context, candidate discovery.Candidate, indices []int, acceptRisk bool) (storage.Server, storage.Endpoint, error) {
	approval, inputs, err := s.approveSelection(candidate, indices, acceptRisk)
	if err != nil {
		return storage.Server{}, storage.Endpoint{}, err
	}
	first := inputs[0]
	input := ServerInput{Protocol: candidate.Protocol, BrokerName: candidate.BrokerName, ServerName: candidate.ServerName,
		IPAddress: first.IPAddress, Port: first.Port}
	return s.createServer(ctx, input, approval, inputs[1:]...)
}

func (s *Service) AddDiscoveredEndpoints(ctx context.Context, serverID string, candidate discovery.Candidate, indices []int, acceptRisk bool) ([]storage.Endpoint, error) {
	approval, inputs, err := s.approveSelection(candidate, indices, acceptRisk)
	if err != nil {
		return nil, err
	}
	return s.addEndpoints(ctx, serverID, inputs, approval)
}

func (s *Service) approveSelection(candidate discovery.Candidate, indices []int, acceptRisk bool) (*endpointApproval, []EndpointInput, error) {
	if len(indices) == 0 || len(indices) > len(candidate.Endpoints) {
		return nil, nil, &InputError{"请选择目录返回的有效接入点"}
	}
	approval, _, err := s.approveCandidate(candidate, indices[0], acceptRisk)
	if err != nil {
		return nil, nil, err
	}
	var inputs []EndpointInput
	seen := map[string]bool{}
	for _, index := range indices {
		if index < 0 || index >= len(candidate.Endpoints) {
			return nil, nil, &InputError{"请选择目录返回的有效接入点"}
		}
		access := candidate.Endpoints[index]
		address := endpointAddress(storage.Endpoint{IPAddress: access.IPAddress, Port: access.Port})
		if seen[address] {
			return nil, nil, &InputError{"采集点地址不能重复"}
		}
		seen[address] = true
		inputs = append(inputs, EndpointInput{IPAddress: access.IPAddress, Port: access.Port})
	}
	return approval, inputs, nil
}

func (s *Service) approveCandidate(candidate discovery.Candidate, index int, acceptRisk bool) (*endpointApproval, discovery.Access, error) {
	if !acceptRisk {
		return nil, discovery.Access{}, &InputError{"请核对经纪商和服务器，并确认本次接入风险"}
	}
	if candidate.ExpiresAtMS <= s.Now().UnixMilli() || candidate.FetchedAtMS > s.Now().Add(time.Minute).UnixMilli() {
		return nil, discovery.Access{}, discovery.ErrExpired
	}
	if index < 0 || index >= len(candidate.Endpoints) {
		return nil, discovery.Access{}, &InputError{"请选择目录返回的有效接入点"}
	}
	digest := sha256.Sum256(candidate.EvidenceBody)
	validSource := candidate.SourceURL == discovery.SourceURL || candidate.SourceURL == discovery.MT5SourceURL
	if !validSource || hex.EncodeToString(digest[:]) != hex.EncodeToString(candidate.EvidenceSHA256) {
		return nil, discovery.Access{}, &InputError{"目录来源证据无效，请重新搜索"}
	}
	return &endpointApproval{ref: candidate.SourceURL + "#sha256=" + hex.EncodeToString(digest[:]), digest: digest[:], source: candidate}, candidate.Endpoints[index], nil
}

func (a *endpointApproval) apply(tx *gorm.DB, endpoint *storage.Endpoint, now int64) error {
	if a == nil {
		return nil
	}
	evidence := struct {
		SHA256      []byte `gorm:"column:sha256"`
		SourceURL   string `gorm:"column:source_url"`
		FetchedAtMS int64  `gorm:"column:fetched_at_ms"`
		Body        []byte `gorm:"column:body"`
	}{a.digest, a.source.SourceURL, a.source.FetchedAtMS, a.source.EvidenceBody}
	if err := tx.Table("server_directory_evidence").Clauses(clause.OnConflict{DoNothing: true}).Create(&evidence).Error; err != nil {
		return err
	}
	endpoint.State = "reviewed"
	endpoint.EvidenceRef = &a.ref
	endpoint.EvidenceSHA256 = a.digest
	endpoint.ReviewedAtMS = &now
	endpoint.RiskAcceptedAtMS = &now
	return nil
}

func (a *endpointApproval) matchesServer(server storage.Server) bool {
	if a == nil {
		return true
	}
	protocol := a.source.Protocol
	if protocol == "" {
		protocol = "mt4"
	}
	return protocol == server.Protocol && strings.EqualFold(server.ServerName, a.source.ServerName) && strings.EqualFold(server.BrokerName, a.source.BrokerName)
}
