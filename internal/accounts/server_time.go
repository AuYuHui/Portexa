package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

var ErrServerTime = errors.New("服务器时间正在自动校准")

type serverTimeRule struct {
	ID           string
	ServerID     string
	Revision     int64
	RulesJSON    string
	EvidenceRef  *string
	VerifiedAtMS *int64
	CreatedAtMS  int64
}

func (serverTimeRule) TableName() string { return "server_time_rule_sets" }

type timeRuleEntry struct {
	FromUTCMS     int64  `json:"from_utc_ms"`
	UntilUTCMS    *int64 `json:"until_utc_ms"`
	Kind          string `json:"kind"`
	Zone          string `json:"zone"`
	SourceRef     string `json:"source_ref"`
	PolicyID      string `json:"policy_id,omitempty"`
	OffsetMinutes int    `json:"offset_minutes,omitempty"`
}

type ServerTimeInput struct {
	PreviousRulesetID string `json:"previous_ruleset_id"`
	TimeZone          string `json:"time_zone"`
	EvidenceRef       string `json:"evidence_ref"`
	Confirm           bool   `json:"confirm"`
	Mode              string `json:"mode"`
}

// ConfirmServerTime appends a version. Prior reports keep their interpretation;
// current reports are re-read under the newly confirmed server calendar.
func (s *Service) ConfirmServerTime(ctx context.Context, id string, input ServerTimeInput) ([]string, error) {
	zone := strings.TrimSpace(input.TimeZone)
	if input.Mode != "" && input.Mode != "manual" && input.Mode != "automatic" {
		return nil, &InputError{"服务器时间设置方式无效"}
	}
	if input.Mode == "" && (!input.Confirm || !clean(input.EvidenceRef, 1, 256)) {
		return nil, &InputError{"请填写核对依据并确认服务器时区"}
	}
	if input.Mode != "automatic" {
		if !clean(zone, 1, 64) || zone == "Local" {
			return nil, &InputError{"请填写明确的服务器时区"}
		}
		if _, err := time.LoadLocation(zone); err != nil {
			return nil, &InputError{"时区名称无效"}
		}
	}
	var ids []string
	err := s.Store.Write(ctx, storage.High, func(tx *gorm.DB) error {
		var server storage.Server
		if err := tx.First(&server, "id = ? AND retired_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		previous := ""
		if server.ActiveTimeRulesetID != nil {
			previous = *server.ActiveTimeRulesetID
		}
		if previous != input.PreviousRulesetID {
			return storage.ErrConflict
		}
		now := s.Now()
		evidence := strings.TrimSpace(input.EvidenceRef)
		if input.Mode == "manual" {
			evidence = "用户手动校正服务器时区"
		}
		entry := timeRuleEntry{FromUTCMS: now.UnixMilli(), Kind: "iana", Zone: zone, SourceRef: evidence}
		if input.Mode == "automatic" {
			if err := tx.Model(&storage.Account{}).Where("server_id = ? AND removed_at_ms IS NULL", server.ID).Updates(map[string]any{
				"next_clock_sync_at_ms": 0, "clock_failure_count": 0, "clock_sync_paused": false,
			}).Error; err != nil {
				return err
			}
			var available bool
			var err error
			entry, available, err = preferredAutomaticTime(tx, server, now)
			if err != nil {
				return err
			}
			if !available {
				if err := clearAutomaticTime(tx, &server); err != nil {
					return err
				}
				return tx.Model(&storage.Account{}).Where("server_id = ? AND removed_at_ms IS NULL", id).Pluck("id", &ids).Error
			}
			evidence = entry.SourceRef
		}
		if err := appendTimeRule(tx, &server, entry, evidence, now.UnixMilli()); err != nil {
			return err
		}
		return tx.Model(&storage.Account{}).Where("server_id = ? AND removed_at_ms IS NULL", id).Pluck("id", &ids).Error
	})
	return ids, err
}

func appendTimeRule(tx *gorm.DB, server *storage.Server, entry timeRuleEntry, evidence string, now int64) error {
	var revision int64
	if err := tx.Model(&serverTimeRule{}).Where("server_id = ?", server.ID).Select("COALESCE(MAX(revision), 0)").Scan(&revision).Error; err != nil {
		return err
	}
	body, err := json.Marshal([]timeRuleEntry{entry})
	if err != nil {
		return err
	}
	rule := serverTimeRule{ID: storage.NewID(), ServerID: server.ID, Revision: revision + 1, RulesJSON: string(body),
		EvidenceRef: &evidence, VerifiedAtMS: &now, CreatedAtMS: now}
	if err = tx.Create(&rule).Error; err != nil {
		return err
	}
	if err = tx.Model(server).Update("active_time_ruleset_id", rule.ID).Error; err != nil {
		return err
	}
	server.ActiveTimeRulesetID = &rule.ID
	return tx.Model(&storage.Account{}).Where("server_id = ? AND removed_at_ms IS NULL", server.ID).Updates(map[string]any{
		"next_daily_sync_at_ms": 0, "daily_failure_count": 0, "daily_sync_paused": false,
		"next_history_sync_at_ms": 0, "history_sync_paused": false,
	}).Error
}

type ServerDay struct {
	Date          string
	TimeZone      string
	TimeRulesetID string
	Start, End    uint32 // Raw MT4 wall-clock seconds, not UTC instants.
	NextDayUTC    time.Time
}

func serverDay(tx *gorm.DB, server storage.Server, now time.Time) (ServerDay, error) {
	if server.ActiveTimeRulesetID == nil {
		return ServerDay{}, ErrServerTime
	}
	var rule serverTimeRule
	if err := tx.First(&rule, "id = ? AND server_id = ?", *server.ActiveTimeRulesetID, server.ID).Error; err != nil {
		return ServerDay{}, err
	}
	if rule.VerifiedAtMS == nil {
		return ServerDay{}, ErrServerTime
	}
	var entries []timeRuleEntry
	if err := json.Unmarshal([]byte(rule.RulesJSON), &entries); err != nil {
		return ServerDay{}, ErrServerTime
	}
	for _, entry := range entries {
		if now.UnixMilli() < entry.FromUTCMS || (entry.UntilUTCMS != nil && now.UnixMilli() >= *entry.UntilUTCMS) {
			continue
		}
		if entry.Kind == "observed_offset" {
			if valid, err := measuredClockValid(tx, server, entry, now); err != nil || !valid {
				if err != nil {
					return ServerDay{}, err
				}
				return ServerDay{}, ErrServerTime
			}
		}
		zone, label, err := serverLocation(entry, now)
		if err != nil {
			return ServerDay{}, ErrServerTime
		}
		local := now.In(zone)
		start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		end := start.AddDate(0, 0, 1)
		if start.Unix() < 0 || end.Unix() > 2147483647 {
			return ServerDay{}, ErrServerTime
		}
		nextDay := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, zone)
		if entry.Kind == "broker_us_dst" {
			// Tomorrow may use a different US offset even though the current
			// server clock is represented by a fixed-offset location.
			nextZone, _, err := serverLocation(entry, nextDay)
			if err != nil {
				return ServerDay{}, err
			}
			nextDay = time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, nextZone)
		}
		return ServerDay{Date: start.Format("2006-01-02"), TimeZone: label, TimeRulesetID: rule.ID,
			Start: uint32(start.Unix()), End: uint32(end.Unix()),
			NextDayUTC: nextDay}, nil
	}
	return ServerDay{}, ErrServerTime
}
