// Package mt5 provides a read-only native TCP client for MetaTrader 5.
package mt5

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"portexa/mt5/internal/protocol"
)

// Config binds a verified endpoint to a specific IP and port.
type Config struct {
	IPAddress          string
	Port               int
	Server             string
	Login              uint64
	Password           string
	InstallationNumber uint64 // Optional: client installation ID (randomly generated if 0)
	DeviceID           string // Optional: 10-char alphanumeric device ID (randomly generated if empty)
	MarketCache        []byte // Optional: market catalog cache descriptor (defaults to verified baseline if nil)
}

var (
	ErrConfig        = errors.New("invalid MT5 connection configuration")
	ErrReadOnly      = errors.New("MT5 investor password required")
	ErrProtocol      = errors.New("unexpected MT5 protocol response")
	ErrRejected      = errors.New("MT5 server rejected the request")
	ErrResourceLimit = errors.New("MT5 response exceeded the message limit")
)

func ValidPassword(password string) bool {
	return utf8.ValidString(password) && password != "" && !strings.ContainsRune(password, 0) &&
		len(utf16.Encode([]rune(password))) <= 32
}

func validConfig(cfg Config) bool {
	ip, err := netip.ParseAddr(cfg.IPAddress)
	if err != nil || ip.Zone() != "" || cfg.Port < 1 || cfg.Port > 65535 ||
		strings.TrimSpace(cfg.Server) == "" || cfg.Login == 0 || !ValidPassword(cfg.Password) {
		return false
	}
	return true
}

// Probe connects to the native endpoint, verifies handshake and double-sided proof.
func Probe(ctx context.Context, cfg Config) (Result, error) {
	if !validConfig(cfg) {
		return Result{}, ErrConfig
	}
	target := net.JoinHostPort(cfg.IPAddress, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()

	session, err := protocol.HandshakeAndAuthenticate(conn, conn, cfg.Login, cfg.Password)
	if err != nil {
		return Result{}, mapProtocolError(err)
	}

	return Result{
		Account: Account{
			Server: cfg.Server,
		},
	}, sessionError(session)
}

func sessionError(session *protocol.AuthSession) error {
	if session == nil {
		return ErrProtocol
	}
	return nil
}

// Fetch authenticates and synchronizes the account funds, open positions and pending orders.
func Fetch(ctx context.Context, cfg Config) (Result, error) {
	if !validConfig(cfg) {
		return Result{}, ErrConfig
	}

	target := net.JoinHostPort(cfg.IPAddress, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()

	session, err := protocol.HandshakeAndAuthenticate(conn, conn, cfg.Login, cfg.Password)
	if err != nil {
		return Result{}, mapProtocolError(err)
	}

	syncOpts := protocol.SyncOptions{
		InstallationNumber: cfg.InstallationNumber,
		DeviceID:           cfg.DeviceID,
		MarketCache:        cfg.MarketCache,
	}
	syncRes, err := protocol.PerformSync(conn, conn, session, cfg.Login, syncOpts)
	if err != nil {
		return Result{}, mapProtocolError(err)
	}

	res := Result{
		Account: Account{
			Server:   cfg.Server,
			Currency: syncRes.Account.Currency,
			Balance:  syncRes.Account.Balance,
			Credit:   syncRes.Account.Credit,
			Leverage: syncRes.Account.Leverage,
			Investor: syncRes.Account.IsInvestor,
		},
	}

	for _, p := range syncRes.Positions {
		res.Positions = append(res.Positions, Position{
			ID:           p.Ticket,
			Symbol:       p.Symbol,
			Side:         uint32(p.Type),
			Digits:       2,
			OpenTime:     p.OpenTime,
			VolumeUnits:  uint64(p.Lots*100000000 + 0.5),
			OpenPrice:    p.OpenPrice,
			CurrentPrice: p.CurrentPrice,
			SL:           p.SL,
			TP:           p.TP,
			Profit:       p.Profit,
			Swap:         p.Swap,
		})
	}

	for _, o := range syncRes.Orders {
		res.Orders = append(res.Orders, PendingOrder{
			Ticket:               o.Ticket,
			Symbol:               o.Symbol,
			Type:                 uint32(o.Type),
			Digits:               2,
			SetupTime:            o.OpenTime,
			RemainingVolumeUnits: uint64(o.Lots*100000000 + 0.5),
			Price:                o.Price,
			SL:                   o.SL,
			TP:                   o.TP,
		})
	}

	return res, nil
}

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

	session, err := protocol.HandshakeAndAuthenticate(conn, conn, cfg.Login, cfg.Password)
	if err != nil {
		return HistoryResult{}, mapProtocolError(err)
	}

	syncOpts := protocol.SyncOptions{
		InstallationNumber: cfg.InstallationNumber,
		DeviceID:           cfg.DeviceID,
		MarketCache:        cfg.MarketCache,
	}
	syncRes, err := protocol.PerformSync(conn, conn, session, cfg.Login, syncOpts)
	if err != nil {
		return HistoryResult{}, mapProtocolError(err)
	}

	startTime := time.Unix(int64(window.Start), 0)
	endTime := time.Unix(int64(window.End), 0)

	dealRecords, err := protocol.FetchHistoryDeals(conn, conn, session.OutState, session.InState, 4, startTime, endTime)
	if err != nil {
		return HistoryResult{}, mapProtocolError(err)
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
			Digits:      2,
			VolumeUnits: uint64(d.Lots * 100000000),
			Price:       d.Price,
			Profit:      d.Profit,
			Commission:  d.Commission,
			Swap:        d.Swap,
			Comment:     d.Comment,
		})
	}

	return res, nil
}

func mapProtocolError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, protocol.ErrAuthFailed) {
		return ErrRejected
	}
	if errors.Is(err, protocol.ErrInvestorDenied) {
		return ErrReadOnly
	}
	if errors.Is(err, protocol.ErrFrameTooLarge) {
		return ErrResourceLimit
	}
	return ErrProtocol
}
