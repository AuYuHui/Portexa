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
	MarketCache        []byte // Optional: override the market catalog cache descriptor.
}

var (
	ErrConfig        = errors.New("invalid MT5 connection configuration")
	ErrReadOnly      = errors.New("MT5 investor password required")
	ErrProtocol      = errors.New("unexpected MT5 protocol response")
	ErrConnection    = errors.New("MT5 connection lost")
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
		return Result{}, sessionProtocolError(ctx, err)
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
	return fetchSnapshot(ctx, cfg, len(cfg.MarketCache) == 0)
}

func fetchSnapshot(ctx context.Context, cfg Config, fullCatalog bool) (Result, error) {
	target := net.JoinHostPort(cfg.IPAddress, strconv.Itoa(cfg.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()

	session, err := protocol.HandshakeAndAuthenticate(conn, conn, cfg.Login, cfg.Password)
	if err != nil {
		return Result{}, sessionProtocolError(ctx, err)
	}

	syncOpts := protocol.SyncOptions{
		InstallationNumber: cfg.InstallationNumber,
		DeviceID:           cfg.DeviceID,
		MarketCache:        cfg.MarketCache,
		FullMarketCatalog:  fullCatalog,
	}
	syncRes, err := protocol.PerformSync(conn, conn, session, cfg.Login, syncOpts)
	if err != nil {
		if fullCatalog && ctx.Err() == nil &&
			(errors.Is(err, protocol.ErrPeerClosed) || errors.Is(err, protocol.ErrFrameTooLarge)) {
			_ = conn.Close()
			return fetchSnapshot(ctx, cfg, false)
		}
		return Result{}, sessionProtocolError(ctx, err)
	}

	res := Result{
		Account: Account{
			Server:          cfg.Server,
			Currency:        syncRes.Account.Currency,
			Balance:         syncRes.Account.Balance,
			Credit:          syncRes.Account.Credit,
			Leverage:        syncRes.Account.Leverage,
			Investor:        syncRes.Account.IsInvestor,
			Margin:          syncRes.Account.Margin,
			MarginAvailable: syncRes.Account.MarginAvailable,
		},
	}

	for _, p := range syncRes.Positions {
		res.Positions = append(res.Positions, Position{
			ID:           p.Ticket,
			Identifier:   p.Identifier,
			NativeTicket: p.NativeTicket,
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

func mapProtocolError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, protocol.ErrAuthFailed) {
		return ErrRejected
	}
	if errors.Is(err, protocol.ErrFrameTooLarge) {
		return ErrResourceLimit
	}
	var socket *net.OpError
	if errors.Is(err, protocol.ErrPeerClosed) || errors.Is(err, net.ErrClosed) || errors.As(err, &socket) {
		return errors.Join(ErrConnection, ErrProtocol)
	}
	return ErrProtocol
}
