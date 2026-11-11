package accounts

import (
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

func automaticClockView(tx *gorm.DB, server storage.Server, now time.Time) (AutomaticTimeZone, error) {
	view := automaticTimeZone(server, now)
	view.RequiredEvents = ClockRequiredEvents
	if view.Source == "" {
		view.Source, view.Status = "learning", "learning"
		view.Description = "等待新平仓交易自动校准；连续 3 笔、覆盖至少 2 轮采集后自动采用，无需选择时区。"
	}
	state, err := clockState(tx, server.ID)
	if err != nil {
		return view, err
	}
	if freshClockState(state, server, now) {
		view.EvidenceCount, view.EventWindowCount = state.EvidenceCount, state.EventWindowCount
		view.Status, view.ObservedAtMS, view.ExpiresAtMS = state.Status, state.ObservedAtMS, state.ExpiresAtMS
		if state.Status == "calibrated" {
			view.Available, view.Source, view.TimeZone = true, "events", clockOffsetLabel(*state.OffsetMinutes)
			view.SourceURL = ""
			view.Description = "已用新平仓交易校准当前时差，后续新交易会持续复核。"
		} else if state.Status == "conflict" {
			_, available, err := preferredAutomaticTime(tx, server, now)
			if err != nil {
				return view, err
			}
			if !available {
				view.Available, view.Source, view.TimeZone, view.SourceURL = false, "learning", "", ""
				view.Description = "新交易时间与原规则不一致，正在等待更多新平仓交易重新校准。"
			}
		}
	}
	return view, nil
}
