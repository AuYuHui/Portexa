package syncer

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/mt4"
	"portexa/mt5"
)

const (
	mt5HistorySetupLimit  = 5 * time.Second
	mt5HistoryBackupFloor = 8 * time.Second
)

var errHistoryRetryBudget = errors.New("insufficient time for a backup history attempt")

func collectionConnectionFailure(err error) bool {
	var failure *mt4.Error
	if errors.As(err, &failure) {
		return failure.Code == mt4.ConnectionFailed || failure.Code == mt4.Timeout
	}
	var socket *net.OpError
	return errors.As(err, &socket) || errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, mt5.ErrConnection) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

type endpointAttempt struct {
	accounts.CollectionTarget
	number       int
	setupTimeout time.Duration
}

func (e *Engine) collect(ctx context.Context, account storage.Account, run any, runID string, target accounts.CollectionTarget, backups []string, setupOnly bool, fetch func(context.Context, endpointAttempt) error) (string, error) {
	_, backfill := run.(*storage.HistoryRun)
	mt5Backfill := backfill && setupOnly
	var err error
	for i := 0; i <= len(backups); i++ {
		if err := ctx.Err(); err != nil {
			return target.ID, err
		}
		options := endpointAttempt{CollectionTarget: target, number: i + 1}
		// Preserve time to connect to backups. MT5 history can use the remainder
		// of the job deadline once the session is ready to read deals.
		attempt, cancel := ctx, func() {}
		if deadline, ok := ctx.Deadline(); ok && i < len(backups) {
			share := time.Until(deadline) / time.Duration(len(backups)+1-i)
			if share <= 0 {
				return target.ID, context.DeadlineExceeded
			}
			if setupOnly {
				options.setupTimeout = share
				if mt5Backfill {
					options.setupTimeout = min(share, mt5HistorySetupLimit)
				}
			} else {
				attempt, cancel = context.WithTimeout(ctx, share)
			}
		}
		err = fetch(attempt, options)
		if err == nil {
			err = attempt.Err()
		}
		cancel()
		if err == nil {
			return target.ID, nil
		}
		failure := classify(err)
		if target.MT5 != nil {
			failure = classifyMT5(err)
		}
		if mt5Backfill && failure.Code == "timeout" && failure.Operation == string(mt5.HistoryDeals) {
			return target.ID, err
		}
		setupTimeout := mt5Backfill && failure.Code == "timeout" && (failure.Operation == string(mt5.HistoryConnect) ||
			failure.Operation == string(mt5.HistoryHandshake) || failure.Operation == string(mt5.HistoryAccountSync))
		if !setupTimeout && !collectionConnectionFailure(err) {
			break
		}
		if deadline, ok := ctx.Deadline(); mt5Backfill && i < len(backups) && ok &&
			!errors.Is(ctx.Err(), context.Canceled) && time.Until(deadline) < mt5HistoryBackupFloor {
			return target.ID, errors.Join(err, errHistoryRetryBudget)
		}
		// Skip a backup that became ineligible after the job was claimed.
		for i < len(backups) && ctx.Err() == nil {
			backup, switchErr := e.accounts.SwitchCollectionTarget(ctx, account, run, runID, backups[i])
			if switchErr == nil {
				e.logger.Info("collection_endpoint_fallback", "run_id", runID, "attempt", i+2, "code", failure.Code, "operation", failure.Operation,
					"from_endpoint_revision", target.Revision, "to_endpoint_revision", backup.Revision)
				target = backup
				break
			}
			if !errors.Is(switchErr, accounts.ErrEndpoint) && !errors.Is(switchErr, accounts.ErrTarget) && !errors.Is(switchErr, storage.ErrNotFound) {
				return target.ID, switchErr
			}
			i++
		}
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				err = ctx.Err()
			}
			break
		}
	}
	return target.ID, err
}
