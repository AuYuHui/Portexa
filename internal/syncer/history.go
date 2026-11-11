package syncer

import (
	"context"
	"errors"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
)

func (e *Engine) scheduleHistory(ctx context.Context) {
	ids, err := e.accounts.HistoryDue(ctx, time.Now().UnixMilli(), 200)
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
		go e.runHistory(jobCtx, id)
	}
}

func (e *Engine) runHistory(ctx context.Context, id string) {
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
	job, err := e.accounts.ClaimHistory(ctx, id)
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
	logger := e.logger.With("account_id", id, "run_id", job.Run.ID, "kind", "history")
	logger.Info("history_sync_started")
	result, err := e.fetch(fetchCtx, job.Config, mt4.Options{History: &mt4.HistoryRange{Start: job.Coverage.StartWall, End: job.Coverage.EndWall}})
	job.Config.Password = ""
	finished := time.Now()
	if err == nil {
		err = fetchCtx.Err()
	}
	if err == nil {
		var orders []storage.HistoryOrder
		orders, err = ConvertHistory(job, result)
		if err == nil {
			err = e.accounts.PublishHistory(fetchCtx, job, orders, finished.UnixMilli())
		}
	}
	if err == nil {
		logger.Info("history_sync_completed", "duration_ms", finished.UnixMilli()-job.Run.StartedAtMS)
		return
	}
	failure := classify(err)
	originalOperation := failure.Operation
	failure.Operation = "history"
	if errors.Is(err, ErrHistoryTooLarge) {
		failure.Code = "history_window_too_large"
	}
	if errors.Is(err, ErrIncomplete) {
		failure.Code = "history_invalid"
	}
	e.mu.Lock()
	yielded := e.historyYielded[id]
	e.mu.Unlock()
	if yielded || (!finished.Before(deadline) && errors.Is(fetchCtx.Err(), context.DeadlineExceeded)) {
		failure = accounts.Failure{Code: "snapshot_deadline", Operation: "history", Canceled: true}
	}
	var protocolFailure *mt4.Error
	historyPhase := originalOperation == "history" || !errors.As(err, &protocolFailure) && result.HistoryComplete
	split := !failure.Canceled && (errors.Is(err, ErrHistoryTooLarge) || historyPhase && (failure.Code == "resource_limit" || failure.Code == "timeout"))
	if split {
		failure.Permanent = false
	}
	logger.Warn("history_sync_failed", "code", failure.Code, "split", split)
	endCtx, endCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer endCancel()
	if err := e.accounts.FailHistory(endCtx, job, failure, split); err != nil {
		e.stopForStorageFailure()
	}
}
