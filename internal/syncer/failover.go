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

func (e *Engine) collect(ctx context.Context, account storage.Account, run any, runID string, target accounts.CollectionTarget, backups []string, fetch func(context.Context, accounts.CollectionTarget) error) (string, error) {
	var err error
	for i := 0; i <= len(backups); i++ {
		if err := ctx.Err(); err != nil {
			return target.ID, err
		}
		// Reserve time for the remaining endpoints within this job's existing deadline.
		attempt, cancel := ctx, func() {}
		if deadline, ok := ctx.Deadline(); ok && i < len(backups) {
			attempt, cancel = context.WithTimeout(ctx, time.Until(deadline)/time.Duration(len(backups)+1-i))
		}
		err = fetch(attempt, target)
		if err == nil {
			err = attempt.Err()
		}
		cancel()
		if !collectionConnectionFailure(err) {
			break
		}
		// Skip a backup that became ineligible after the job was claimed.
		for i < len(backups) && ctx.Err() == nil {
			backup, switchErr := e.accounts.SwitchCollectionTarget(ctx, account, run, runID, backups[i])
			if switchErr == nil {
				failure := classify(err)
				if target.MT5 != nil {
					failure = classifyMT5(err)
				}
				e.logger.Info("collection_endpoint_fallback", "run_id", runID, "attempt", i+2, "code", failure.Code, "operation", failure.Operation)
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
