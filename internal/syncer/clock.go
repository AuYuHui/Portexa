package syncer

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

// ConvertClock only accepts a complete response for the entire bootstrap
// window. An old historical row cannot become evidence without a separately
// recorded position disappearance in accounts.PublishClock.
func ConvertClock(job accounts.ClockJob, result mt4.Result) ([]accounts.ClosedClockTrade, error) {
	if !result.SessionConfirmed || !result.AccountRead || !result.HistoryRead || !result.HistoryComplete ||
		!coversDay(result.HistoryCovered, job.Run.WindowStart, job.Run.WindowEnd) {
		return nil, ErrIncomplete
	}
	trades := make([]accounts.ClosedClockTrade, 0)
	seen := make(map[int32]bool, len(result.History))
	for _, trade := range result.History {
		if trade.Ticket <= 0 || seen[trade.Ticket] || trade.Command < 0 || trade.Command > 7 {
			return nil, ErrIncomplete
		}
		seen[trade.Ticket] = true
		if trade.Command > 1 {
			continue
		}
		if trade.CloseTime == 0 {
			return nil, ErrIncomplete
		}
		if trade.CloseTime < job.Run.WindowStart || trade.CloseTime >= job.Run.WindowEnd {
			continue
		}
		trades = append(trades, accounts.ClosedClockTrade{Ticket: trade.Ticket, Command: trade.Command,
			Symbol: trade.Symbol, OpenTime: trade.OpenTime, CloseTime: trade.CloseTime})
	}
	return trades, nil
}

func (e *Engine) scheduleClock(ctx context.Context) {
	ids, err := e.accounts.ClockDue(ctx, time.Now().UnixMilli(), 200)
	if err != nil {
		if ctx.Err() == nil {
			e.stopForStorageFailure()
		}
		return
	}
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		e.mu.Lock()
		if e.active[id] != nil {
			e.mu.Unlock()
			continue
		}
		select {
		case e.historySem <- struct{}{}:
		default:
			e.mu.Unlock()
			return
		}
		jobCtx, cancel := context.WithTimeout(ctx, e.timeout)
		e.active[id], e.historyActive[id] = cancel, true
		e.wg.Add(1)
		e.mu.Unlock()
		go e.runClock(jobCtx, id)
	}
}

func (e *Engine) runClock(ctx context.Context, id string) {
	defer func() {
		e.mu.Lock()
		e.active[id]()
		delete(e.active, id)
		delete(e.historyActive, id)
		delete(e.historyYielded, id)
		e.mu.Unlock()
		<-e.historySem
		e.wg.Done()
	}()
	job, err := e.accounts.ClaimClock(ctx, id)
	if err != nil {
		if !accounts.IsBrokenGate(err) && !errors.Is(err, accounts.ErrPaused) && !errors.Is(err, accounts.ErrServerTime) && !errors.Is(err, storage.ErrNotFound) && ctx.Err() == nil {
			e.stopForStorageFailure()
		}
		return
	}
	deadline := time.UnixMilli(job.Account.NextSyncAtMS).Add(-accounts.DailySnapshotGuard)
	fetchCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	logger := e.logger.With("account_id", id, "run_id", job.Run.ID, "kind", "clock")
	logger.Info("clock_calibration_started")
	result, err := e.fetch(fetchCtx, job.Config, mt4.Options{History: &mt4.HistoryRange{Start: job.Run.WindowStart, End: job.Run.WindowEnd}})
	job.Config.Password = ""
	finished := time.Now()
	if err == nil {
		err = fetchCtx.Err()
	}
	if err == nil {
		var trades []accounts.ClosedClockTrade
		trades, err = ConvertClock(job, result)
		if err == nil {
			err = e.accounts.PublishClock(fetchCtx, job, trades, finished.UnixMilli())
		}
	}
	if err == nil {
		logger.Info("clock_calibration_completed", "duration_ms", finished.UnixMilli()-job.Run.StartedAtMS)
		return
	}
	failure := classify(err)
	failure.Operation = "clock_history"
	if failure.Code == "snapshot_invalid" {
		failure.Code = "calibration_invalid"
	}
	e.mu.Lock()
	yielded := e.historyYielded[id]
	e.mu.Unlock()
	if yielded || (!finished.Before(deadline) && errors.Is(fetchCtx.Err(), context.DeadlineExceeded)) {
		failure = accounts.Failure{Code: "snapshot_deadline", Operation: "clock_history", Canceled: true}
	}
	level := slog.LevelWarn
	if failure.Canceled {
		level = slog.LevelInfo
	}
	logger.Log(ctx, level, "clock_calibration_failed", "code", failure.Code)
	endCtx, endCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer endCancel()
	if err := e.accounts.FailClock(endCtx, job, failure); err != nil {
		e.stopForStorageFailure()
	}
}
