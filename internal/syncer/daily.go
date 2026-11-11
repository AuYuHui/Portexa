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

func (e *Engine) scheduleDaily(ctx context.Context) {
	ids, err := e.accounts.DailyDue(ctx, time.Now().UnixMilli(), 200)
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
		go e.runDaily(jobCtx, id)
	}
}

func (e *Engine) runDaily(ctx context.Context, id string) {
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
	job, err := e.accounts.ClaimDaily(ctx, id)
	if err != nil {
		if accounts.IsBrokenGate(err) {
			endCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := e.accounts.DisableBrokenGate(endCtx, id, job.Account.ConfigVersion); err != nil && !errors.Is(err, storage.ErrNotFound) {
				e.stopForStorageFailure()
			}
		} else if !errors.Is(err, accounts.ErrPaused) && !errors.Is(err, accounts.ErrServerTime) && !errors.Is(err, storage.ErrNotFound) && ctx.Err() == nil {
			e.stopForStorageFailure()
		}
		return
	}
	deadline := time.UnixMilli(job.Account.NextSyncAtMS).Add(-accounts.DailySnapshotGuard)
	fetchCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	logger := e.logger.With("account_id", id, "run_id", job.Run.ID, "kind", "daily")
	logger.Info("daily_sync_started")
	result, err := e.fetch(fetchCtx, job.Config, mt4.Options{History: &mt4.HistoryRange{Start: job.Day.Start, End: job.Day.End}})
	job.Config.Password = ""
	finished := time.Now()
	if err == nil {
		err = fetchCtx.Err()
	}
	if err == nil {
		var report storage.DailyReport
		report, err = ConvertDaily(job, result, finished.UnixMilli())
		if err == nil {
			next := finished.Add(e.historyInterval)
			if job.Day.NextDayUTC.Before(next) {
				next = job.Day.NextDayUTC
			}
			err = e.accounts.PublishDaily(fetchCtx, job, report, next.UnixMilli())
		}
	}
	if err != nil {
		failure := classify(err)
		if failure.Operation == "snapshot" {
			failure.Operation = "history"
		}
		e.mu.Lock()
		yielded := e.historyYielded[id]
		e.mu.Unlock()
		if yielded || (!finished.Before(deadline) && errors.Is(fetchCtx.Err(), context.DeadlineExceeded)) {
			failure = accounts.Failure{Code: "snapshot_deadline", Operation: "history", Canceled: true}
		}
		level := slog.LevelWarn
		if failure.Canceled {
			level = slog.LevelInfo
		}
		logger.Log(ctx, level, "daily_sync_failed", "code", failure.Code, "duration_ms", finished.UnixMilli()-job.Run.StartedAtMS)
		endCtx, endCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer endCancel()
		if err = e.accounts.FailDaily(endCtx, job, failure); err != nil {
			e.stopForStorageFailure()
		}
	} else {
		logger.Info("daily_sync_completed", "duration_ms", finished.UnixMilli()-job.Run.StartedAtMS)
	}
}
