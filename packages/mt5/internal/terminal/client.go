package terminal

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/gorilla/websocket"
)

type Config struct {
	IPAddress string
	Port      int
	Hostname  string
	Server    string
	Login     uint64
	Password  string
}

var (
	ErrReadOnly       = errors.New("MT5 investor password required")
	ErrServerMismatch = errors.New("MT5 trade server does not match")
	ErrProtocol       = errors.New("unexpected MT5 protocol response")
	ErrRejected       = errors.New("MT5 terminal rejected the request")
	ErrResourceLimit  = errors.New("MT5 response exceeded the message limit")
)

const (
	cmdBootstrap = 0
	cmdAccount   = 3
	cmdPositions = 4
	cmdHistory   = 5
	cmdLogin     = 28
	maxMessage   = 4 << 20
)

func Fetch(ctx context.Context, cfg Config, roots *x509.CertPool) (Result, error) {
	return fetch(ctx, cfg, roots, nil)
}

func FetchHistory(ctx context.Context, cfg Config, window HistoryRange, roots *x509.CertPool) (HistoryResult, error) {
	result, err := fetch(ctx, cfg, roots, &window)
	if err != nil {
		return HistoryResult{}, err
	}
	return HistoryResult{Account: result.Account, Deals: result.deals}, nil
}

func fetch(ctx context.Context, cfg Config, roots *x509.CertPool, history *HistoryRange) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()

	port := strconv.Itoa(cfg.Port)
	address := net.JoinHostPort(cfg.IPAddress, port)
	host := net.JoinHostPort(cfg.Hostname, port)
	if cfg.Port == 443 {
		host = cfg.Hostname
	}
	endpoint := url.URL{Scheme: "wss", Host: host, Path: "/terminal"}
	origin := url.URL{Scheme: "https", Host: host}
	network := &net.Dialer{}
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{ServerName: cfg.Hostname, RootCAs: roots, MinVersion: tls.VersionTLS12},
		NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return network.DialContext(ctx, "tcp", address)
		},
	}
	conn, _, err := dialer.DialContext(ctx, endpoint.String(), map[string][]string{"Origin": {origin.String()}})
	if err != nil {
		return Result{}, err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	limit := int64(maxMessage)
	if history != nil {
		limit *= 4
	}
	conn.SetReadLimit(limit)
	if err = conn.SetReadDeadline(deadline); err != nil {
		return Result{}, err
	}
	if err = conn.SetWriteDeadline(deadline); err != nil {
		return Result{}, err
	}
	result, err := readAccountState(conn, cfg, history)
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if errors.Is(err, websocket.ErrReadLimit) {
		return Result{}, ErrResourceLimit
	}
	return result, err
}

func readAccountState(conn *websocket.Conn, cfg Config, history *HistoryRange) (Result, error) {
	session := session{conn: conn, key: initialKey()}
	body, err := session.request(cmdBootstrap, make([]byte, 64))
	if err != nil {
		return Result{}, err
	}
	if len(body) != 66+32 {
		return Result{}, ErrProtocol
	}
	session.key = body[66:]

	login := makeLogin(cfg)
	reply, err := session.request(cmdLogin, login)
	clear(login)
	if err != nil {
		return Result{}, err
	}
	if len(reply) != 168 {
		return Result{}, ErrProtocol
	}

	body, err = session.request(cmdAccount, nil)
	if err != nil {
		return Result{}, err
	}
	account, err := decodeAccount(body)
	if err != nil {
		return Result{}, err
	}
	if !account.investor {
		return Result{}, ErrReadOnly
	}
	if !strings.EqualFold(account.Server, cfg.Server) {
		return Result{}, ErrServerMismatch
	}
	if history != nil {
		deals, err := session.readHistory(*history)
		if err != nil {
			return Result{}, err
		}
		return Result{Account: account, deals: deals}, nil
	}
	body, err = session.request(cmdPositions, nil)
	if err != nil {
		return Result{}, err
	}
	positions, orders, err := decodeOpenState(body)
	if err != nil {
		return Result{}, err
	}
	return Result{Account: account, Positions: positions, Orders: orders}, nil
}

func makeLogin(cfg Config) []byte {
	payload := make([]byte, 920)
	_, _ = rand.Read(payload[196:212])
	writeText(payload[4:68], cfg.Password)
	le.PutUint32(payload[476:480], uint32(len(cfg.Hostname)))
	writeText(payload[480:736], cfg.Hostname)
	le.PutUint64(payload[736:744], cfg.Login)
	return payload
}

func writeText(dst []byte, value string) {
	for i, unit := range utf16.Encode([]rune(value)) {
		le.PutUint16(dst[i*2:], unit)
	}
}
