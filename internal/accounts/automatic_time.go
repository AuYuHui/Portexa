package accounts

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

const dprimeTimePolicy = "dprime-us-dst-v1"
const dprimeTimeSource = "https://www.dooprimenews.com/service-updates/trading-schedule-update-us-daylight-saving-time-shift-and-all-souls-day"

var dprimeBroker = regexp.MustCompile(`(?i)^(?:d|doo) prime(?: (?:vanuatu|seychelles|mauritius) limited)?$`)
var dprimeServer = regexp.MustCompile(`(?i)^(?:dprime|dooprime)(?:vu|sc|mu)?-(?:live|demo)(?:[ -]?[0-9]{1,3})?$`)

type AutomaticTimeZone struct {
	Available        bool   `json:"available"`
	TimeZone         string `json:"time_zone"`
	Description      string `json:"description"`
	SourceURL        string `json:"source_url"`
	Source           string `json:"source"`
	Status           string `json:"status"`
	EvidenceCount    int    `json:"evidence_count"`
	EventWindowCount int    `json:"event_window_count"`
	RequiredEvents   int    `json:"required_events"`
	ObservedAtMS     *int64 `json:"observed_at_ms"`
	ExpiresAtMS      *int64 `json:"expires_at_ms"`
}

// Broker policy is a business calendar, not an inferred protocol timestamp.
// D Prime's published platform clock follows US DST: UTC+2 / UTC+3. In
// particular, Europe/Helsinki would be wrong during the US/EU transition weeks.
func automaticTimeRule(server storage.Server, now time.Time) (timeRuleEntry, bool) {
	if server.Protocol != "mt4" || !dprimeBroker.MatchString(strings.TrimSpace(server.BrokerName)) ||
		!dprimeServer.MatchString(strings.TrimSpace(server.ServerName)) {
		return timeRuleEntry{}, false
	}
	return timeRuleEntry{FromUTCMS: now.UnixMilli(), Kind: "broker_us_dst", Zone: "America/New_York",
		PolicyID: dprimeTimePolicy, SourceRef: dprimeTimeSource}, true
}

func automaticTimeZone(server storage.Server, now time.Time) AutomaticTimeZone {
	entry, ok := automaticTimeRule(server, now)
	if !ok {
		return AutomaticTimeZone{}
	}
	_, label, err := serverLocation(entry, now)
	if err != nil {
		return AutomaticTimeZone{}
	}
	return AutomaticTimeZone{Available: true, TimeZone: label,
		Description: "按 D Prime 官方规则，冬令时 UTC+2、夏令时 UTC+3，随美国夏令时自动切换。新平仓事件会继续校准当前时差。", SourceURL: entry.SourceRef,
		Source: "official", Status: "learning", RequiredEvents: ClockRequiredEvents}
}

func serverLocation(entry timeRuleEntry, at time.Time) (*time.Location, string, error) {
	if entry.Kind == "observed_offset" && entry.PolicyID == "trade-events-v1" && entry.OffsetMinutes >= -720 && entry.OffsetMinutes <= 840 && entry.OffsetMinutes%15 == 0 {
		label := clockOffsetLabel(entry.OffsetMinutes)
		return time.FixedZone(label, entry.OffsetMinutes*60), label, nil
	}
	if entry.Kind == "iana" && entry.Zone != "Local" {
		zone, err := time.LoadLocation(entry.Zone)
		if err == nil {
			return zone, entry.Zone, nil
		}
	}
	if entry.Kind == "broker_us_dst" && entry.PolicyID == dprimeTimePolicy && entry.Zone == "America/New_York" {
		newYork, err := time.LoadLocation(entry.Zone)
		if err != nil {
			return nil, "", err
		}
		hours := 2
		if at.In(newYork).IsDST() {
			hours = 3
		}
		label := fmt.Sprintf("UTC+%d", hours)
		return time.FixedZone(label, hours*3600), label, nil
	}
	return nil, "", ErrServerTime
}

func ensureAutomaticTime(tx *gorm.DB, server *storage.Server, now time.Time) error {
	var current *timeRuleEntry
	if server.ActiveTimeRulesetID != nil {
		var active serverTimeRule
		if err := tx.First(&active, "id = ? AND server_id = ?", *server.ActiveTimeRulesetID, server.ID).Error; err != nil {
			return err
		}
		if active.VerifiedAtMS != nil {
			var entries []timeRuleEntry
			if json.Unmarshal([]byte(active.RulesJSON), &entries) != nil || len(entries) != 1 || entries[0].Kind == "iana" {
				return nil // Explicit manual settings remain in force.
			}
			current = &entries[0]
		}
	}
	entry, available, err := preferredAutomaticTime(tx, *server, now)
	if err != nil {
		return err
	}
	if !available {
		return clearAutomaticTime(tx, server)
	}
	if current != nil && current.Kind == entry.Kind && current.PolicyID == entry.PolicyID && current.Zone == entry.Zone && current.OffsetMinutes == entry.OffsetMinutes {
		return nil
	}
	return appendTimeRule(tx, server, entry, entry.SourceRef, now.UnixMilli())
}

func clockOffsetLabel(minutes int) string {
	if minutes == 0 {
		return "UTC"
	}
	sign := "+"
	if minutes < 0 {
		sign, minutes = "-", -minutes
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("UTC%s%d", sign, minutes/60)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, minutes/60, minutes%60)
}

func clockState(tx *gorm.DB, serverID string) (storage.ClockCalibration, error) {
	var state storage.ClockCalibration
	err := tx.First(&state, "server_id = ?", serverID).Error
	if errors.Is(err, storage.ErrNotFound) {
		err = nil
	}
	return state, err
}

func freshClockState(state storage.ClockCalibration, server storage.Server, now time.Time) bool {
	return state.OffsetMinutes != nil && state.ObservedAtMS != nil && state.ExpiresAtMS != nil &&
		*state.ObservedAtMS <= now.UnixMilli() && now.UnixMilli() < *state.ExpiresAtMS &&
		sameOfficialClockPeriod(server, time.UnixMilli(*state.ObservedAtMS), now)
}

func preferredAutomaticTime(tx *gorm.DB, server storage.Server, now time.Time) (timeRuleEntry, bool, error) {
	state, err := clockState(tx, server.ID)
	if err != nil {
		return timeRuleEntry{}, false, err
	}
	if freshClockState(state, server, now) {
		if state.Status == "calibrated" {
			return timeRuleEntry{FromUTCMS: now.UnixMilli(), Kind: "observed_offset", OffsetMinutes: *state.OffsetMinutes,
				PolicyID: "trade-events-v1", SourceRef: "新平仓事件与两次采集的 UTC 时间窗口交叉校准"}, true, nil
		}
		// A new observed offset contradicts the official clock. Wait for enough
		// new evidence rather than continuing to publish a known disputed day.
		if official, ok := automaticTimeRule(server, now); ok {
			zone, _, err := serverLocation(official, now)
			if err != nil {
				return timeRuleEntry{}, false, err
			}
			_, seconds := now.In(zone).Zone()
			if seconds/60 != *state.OffsetMinutes {
				return timeRuleEntry{}, false, nil
			}
		}
	}
	entry, available := automaticTimeRule(server, now)
	return entry, available, nil
}

func measuredClockValid(tx *gorm.DB, server storage.Server, entry timeRuleEntry, now time.Time) (bool, error) {
	state, err := clockState(tx, server.ID)
	return err == nil && state.Status == "calibrated" && freshClockState(state, server, now) &&
		*state.OffsetMinutes == entry.OffsetMinutes, err
}

func clearAutomaticTime(tx *gorm.DB, server *storage.Server) error {
	if server.ActiveTimeRulesetID == nil {
		return nil
	}
	if err := tx.Model(server).Update("active_time_ruleset_id", nil).Error; err != nil {
		return err
	}
	server.ActiveTimeRulesetID = nil
	return tx.Model(&storage.Account{}).Where("server_id = ? AND removed_at_ms IS NULL", server.ID).Update("next_daily_sync_at_ms", 0).Error
}
