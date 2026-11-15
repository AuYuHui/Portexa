package accounts

import (
	"context"
	"time"

	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

type RestLearningView struct {
	storage.MarketLearningGroup
	ServerName string                       `json:"server_name"`
	Candidate  *storage.MarketLearningModel `json:"candidate,omitempty"`
	Active     *storage.MarketLearningModel `json:"active,omitempty"`
}

// Status is a read model; it cannot activate models or mutate evidence.
func (s *Service) RestLearning(ctx context.Context) ([]RestLearningView, error) {
	out := []RestLearningView{}
	settings, err := s.CollectionSettings(ctx)
	if err != nil {
		return out, err
	}
	var groups []storage.MarketLearningGroup
	if err = s.Store.Read.WithContext(ctx).Where("epoch_ms=?", settings.MarketRestLearningEpochMS).Order("server_id,symbol,id").Limit(200).Find(&groups).Error; err != nil {
		return out, err
	}
	for _, g := range groups {
		v := RestLearningView{MarketLearningGroup: g}
		var server storage.Server
		if err = s.Store.Read.WithContext(ctx).First(&server, "id=?", g.ServerID).Error; err != nil {
			return out, err
		}
		v.ServerName = server.ServerName

		for _, entry := range []struct {
			id     string
			target **storage.MarketLearningModel
		}{{g.CandidateModelID, &v.Candidate}, {g.ActiveModelID, &v.Active}} {
			if entry.id == "" {
				continue
			}
			var model storage.MarketLearningModel
			if err = s.Store.Read.WithContext(ctx).First(&model, "id=?", entry.id).Error; err != nil {
				return out, err
			}
			*entry.target = &model
		}
		if v.Active != nil && v.Active.ExpiresAtMS <= s.Now().UnixMilli() {
			v.State, v.Reason = "suspended", "stale"
		}
		if !settings.MarketRestLearningEnabled || settings.MarketRestTemplate != marketrest.Template {
			v.State, v.Reason = "off", "disabled"
		}
		if g.LastObservedMS < s.Now().Add(-24*time.Hour).UnixMilli() && v.State == "learning" {
			v.Reason = "no_quote"
		}
		out = append(out, v)
	}
	return out, nil
}
