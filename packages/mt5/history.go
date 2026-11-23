package mt5

import (
	"context"
	"errors"
	"net"
	"strconv"
	"time"

	"portexa/mt5/internal/protocol"
)

// FetchHistory returns deals in the half-open wall-clock window.
func FetchHistory(ctx context.Context, cfg Config, window HistoryRange) (HistoryResult, error) {
	result, err := FetchHistoryWithOptions(ctx, cfg, window, HistoryOptions{})
	if phase, ok := err.(*HistoryError); ok {
		err = phase.Err
	}
	return result, err
}

type HistoryPhase string

const (
	HistoryConnect     HistoryPhase = "connect"
	HistoryHandshake   HistoryPhase = "handshake"
	HistoryAccountSync HistoryPhase = "account_sync"
	HistoryDeals       HistoryPhase = "deals"
)

type HistoryOptions struct {
	// SetupTimeout covers TCP, authentication and account synchronization.
	// A zero value uses the caller's deadline for the entire request.
	SetupTimeout time.Duration
	OnPhase      func(HistoryPhase, time.Duration, error)
}

type HistoryError struct {
	Phase HistoryPhase
	Err   error
}

func (e *HistoryError) Error() string { return "mt5 history " + string(e.Phase) + ": " + e.Err.Error() }
func (e *HistoryError) Unwrap() error { return e.Err }

func (options HistoryOptions) finishPhase(phase HistoryPhase, started time.Time, err error) error {
	if options.OnPhase != nil {
		options.OnPhase(phase, time.Since(started), err)
	}
	if err != nil {
		return &HistoryError{Phase: phase, Err: err}
	}
	return nil
}

// FetchHistoryWithOptions allows a separate connection setup budget while
// keeping the full caller deadline for a session that is ready to read deals.
func FetchHistoryWithOptions(ctx context.Context, cfg Config, window HistoryRange, options HistoryOptions) (HistoryResult, error) {
	if !validConfig(cfg) || window.Start >= window.End || window.End > 1<<31-1 {
		return HistoryResult{}, ErrConfig
	}

	setupCtx := ctx
	if options.SetupTimeout > 0 {
		var cancel context.CancelFunc
		setupCtx, cancel = context.WithTimeout(ctx, options.SetupTimeout)
		defer cancel()
	}
	target := net.JoinHostPort(cfg.IPAddress, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: 20 * time.Second}
	started := time.Now()
	conn, err := d.DialContext(setupCtx, "tcp", target)
	if err = options.finishPhase(HistoryConnect, started, err); err != nil {
		return HistoryResult{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	started = time.Now()
	if options.SetupTimeout > 0 {
		deadline, _ := setupCtx.Deadline()
		if err = conn.SetDeadline(deadline); err != nil {
			return HistoryResult{}, options.finishPhase(HistoryHandshake, started, sessionProtocolError(setupCtx, err))
		}
	}
	session, err := protocol.HandshakeAndAuthenticate(conn, conn, cfg.Login, cfg.Password)
	if err = options.finishPhase(HistoryHandshake, started, sessionProtocolError(setupCtx, err)); err != nil {
		return HistoryResult{}, err
	}

	syncOpts := protocol.SyncOptions{
		InstallationNumber: cfg.InstallationNumber,
		DeviceID:           cfg.DeviceID,
		MarketCache:        cfg.MarketCache,
	}
	started = time.Now()
	syncRes, err := protocol.PerformSync(conn, conn, session, cfg.Login, syncOpts)
	err = sessionProtocolError(setupCtx, err)
	if err == nil && options.SetupTimeout > 0 {
		err = conn.SetDeadline(time.Time{})
	}
	if err = options.finishPhase(HistoryAccountSync, started, err); err != nil {
		return HistoryResult{}, err
	}

	started = time.Now()
	dealRecords, err := protocol.FetchHistoryDeals(conn, session.OutState, session.InState, 4, cfg.Login, window.Start, window.End)
	if err = options.finishPhase(HistoryDeals, started, sessionProtocolError(ctx, err)); err != nil {
		return HistoryResult{}, err
	}

	res := HistoryResult{
		Account: Account{
			Server:   cfg.Server,
			Currency: syncRes.Account.Currency,
			Balance:  syncRes.Account.Balance,
			Credit:   syncRes.Account.Credit,
			Leverage: syncRes.Account.Leverage,
			Investor: syncRes.Account.IsInvestor,
		},
	}

	for _, d := range dealRecords {
		res.Deals = append(res.Deals, Deal{
			Ticket:      d.Ticket,
			Order:       d.Order,
			PositionID:  d.Position,
			WallTimeMS:  d.Time,
			Symbol:      d.Symbol,
			Type:        DealType(d.Type),
			Entry:       DealEntry(d.Entry),
			Digits:      d.Digits,
			VolumeUnits: d.VolumeUnits,
			Price:       d.Price,
			SL:          d.SL,
			TP:          d.TP,
			Profit:      d.Profit,
			Commission:  d.Commission,
			Swap:        d.Swap,
			Fee:         d.Fee,
			Comment:     d.Comment,
		})
	}

	return res, nil
}

func sessionProtocolError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return err
	}
	return mapProtocolError(err)
}
