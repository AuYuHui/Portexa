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
	if !validConfig(cfg) || window.Start >= window.End || window.End > 1<<31-1 {
		return HistoryResult{}, ErrConfig
	}

	target := net.JoinHostPort(cfg.IPAddress, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: 20 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return HistoryResult{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	session, err := protocol.HandshakeAndAuthenticate(conn, conn, cfg.Login, cfg.Password)
	if err != nil {
		return HistoryResult{}, historyProtocolError(ctx, err)
	}

	syncOpts := protocol.SyncOptions{
		InstallationNumber: cfg.InstallationNumber,
		DeviceID:           cfg.DeviceID,
		MarketCache:        cfg.MarketCache,
	}
	syncRes, err := protocol.PerformSync(conn, conn, session, cfg.Login, syncOpts)
	if err != nil {
		return HistoryResult{}, historyProtocolError(ctx, err)
	}

	dealRecords, err := protocol.FetchHistoryDeals(conn, session.OutState, session.InState, 4, cfg.Login, window.Start, window.End)
	if err != nil {
		return HistoryResult{}, historyProtocolError(ctx, err)
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

func historyProtocolError(ctx context.Context, err error) error {
	if cause := ctx.Err(); cause != nil {
		return cause
	}
	var network net.Error
	if errors.As(err, &network) && network.Timeout() {
		return err
	}
	return mapProtocolError(err)
}
