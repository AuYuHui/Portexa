package mt4

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"portexa/mt4/internal/protocol"
)

const sessionTimeout = 20 * time.Second

// Probe opens a fresh connection and decodes the first server response only.
// The initial packet already contains password-derived material.
func Probe(ctx context.Context, config Config) (Result, error) {
	conn, err := open(ctx, config, false)
	if err != nil {
		return Result{}, err
	}
	defer conn.close()
	return initialResult(conn.response), nil
}

// Fetch authenticates, synchronizes read-only data, then closes the connection.
// The deadline is the earlier of the caller's deadline and 20 seconds. No trade
// commands, retries, filesystem access, logging or background sessions are used.
func Fetch(ctx context.Context, config Config, options Options) (Result, error) {
	if options.History != nil {
		if _, err := protocol.HistoryRequest(options.History.Start, options.History.End); err != nil {
			return Result{}, &Error{Code: InvalidConfig, Operation: "history", cause: err}
		}
	}
	if options.HistoryWindowSeconds == 1 || (options.History == nil && options.HistoryWindowSeconds != 0) {
		return Result{}, &Error{Code: InvalidConfig, Operation: "history"}
	}
	conn, err := open(ctx, config, true)
	if err != nil {
		return Result{}, err
	}
	defer conn.close()
	result := initialResult(conn.response)
	device, err := protocol.DefaultDeviceInfo()
	if err != nil {
		return result, classifyError("authentication", result.ServerBuild, err)
	}
	auth, _, err := protocol.AuthenticationPacket(conn.response, config.Login, device)
	if err != nil {
		return result, classifyError("authentication", result.ServerBuild, err)
	}
	if _, err := io.Copy(conn.socket, bytes.NewReader(auth)); err != nil {
		return result, conn.error("authentication", err)
	}
	if err := protocol.WriteCommand(conn.socket, &conn.response.Tx, []byte{0x10}); err != nil {
		return result, conn.error("configuration", err)
	}
	configuration, err := protocol.ReadServerConfiguration(conn.socket, &conn.response.Rx, config.Server)
	if err != nil {
		return result, conn.error("configuration", err)
	}
	result.Server = ServerInfo{configuration.Name, len(configuration.Data), configuration.AccessCount}
	result.SessionConfirmed = true
	var start, end uint32
	if options.History != nil {
		start, end = options.History.Start, options.History.End
	}
	session, err := protocol.ReadLiveAccount(conn.socket, &conn.response, start, end, options.HistoryWindowSeconds)
	applySession(&result, session)
	return result, conn.error("synchronization", err)
}

type connection struct {
	socket   net.Conn
	response protocol.InitialResponse
	ctx      context.Context
	cancel   context.CancelFunc
	stop     func() bool
}

func open(ctx context.Context, config Config, requireServer bool) (*connection, error) {
	if config.PasswordType != InvestorPassword {
		return nil, &Error{Code: ReadOnlyRequired, Operation: "config"}
	}
	if config.Login == 0 || len(config.Password) < 1 || len(config.Password) > 16 || strings.ContainsRune(config.Password, 0) {
		return nil, &Error{Code: InvalidConfig, Operation: "config", cause: errors.New("login and a password of 1 to 16 bytes are required")}
	}
	if requireServer && config.Server == "" {
		return nil, &Error{Code: InvalidConfig, Operation: "config", cause: errors.New("a verified server display name is required")}
	}
	host, port, splitErr := net.SplitHostPort(config.Address)
	number, portErr := strconv.Atoi(port)
	if splitErr != nil || net.ParseIP(host) == nil || portErr != nil || number < 1 || number > 65535 {
		return nil, &Error{Code: InvalidConfig, Operation: "config", cause: errors.New("target must be an independently observed IP address and TCP port")}
	}
	ctx, cancel := context.WithTimeout(ctx, sessionTimeout)
	socket, err := (&net.Dialer{}).DialContext(ctx, "tcp", config.Address)
	if err != nil {
		cancel()
		return nil, classifyError("connect", 0, err)
	}
	conn := &connection{socket: socket, ctx: ctx, cancel: cancel}
	conn.stop = context.AfterFunc(ctx, func() { _ = socket.Close() })
	deadline, _ := ctx.Deadline()
	if err = socket.SetDeadline(deadline); err == nil {
		_, err = io.Copy(socket, bytes.NewReader(protocol.InitialPacket(config.Login, config.Password, 0)))
	}
	if err == nil {
		conn.response, err = protocol.ReadInitial(socket, config.Login, config.Password)
	}
	if err != nil {
		err = conn.error("initial", err)
		conn.close()
		return nil, err
	}
	return conn, nil
}

func (c *connection) error(operation string, err error) error {
	var phase *protocol.OperationError
	if errors.As(err, &phase) {
		operation = phase.Operation
	}
	if err != nil && c.ctx.Err() != nil {
		err = c.ctx.Err()
	}
	return classifyError(operation, c.response.ServerBuild, err)
}

func (c *connection) close() {
	c.stop()
	c.cancel()
	_ = c.socket.Close()
}

func initialResult(response protocol.InitialResponse) Result {
	result := Result{ServerBuild: response.ServerBuild, InitialBytes: 28, FirstResponseDecoded: true}
	for _, record := range response.Records {
		result.FeatureRecords = append(result.FeatureRecords, RecordInfo{record.Kind, record.Mode, len(record.Data)})
	}
	return result
}

func applySession(result *Result, session protocol.SessionResult) {
	result.AccountRead, result.HistoryRead = session.AccountRead, session.HistoryRead
	result.Account = Account{
		Leverage: session.Account.Leverage, Balance: session.Account.Balance, Credit: session.Account.Credit,
		InvestorIndicator: session.Account.InvestorIndicator, DemoIndicator: session.Account.DemoIndicator,
		Currency: session.Account.Currency, Equity: session.Account.Equity,
		Margin: session.Account.Margin, MarginAvailable: session.Account.MarginAvailable,
	}
	result.AccountBaseBytes = min(protocol.AccountBaseSize, len(session.Account.Raw))
	result.TradeTrailingBytes = session.Account.TrailingBytes
	result.Trades, result.History = trades(session.Account.Orders), trades(session.History.Records)
	result.HistoryStatus = session.History.Status
	result.HistoryComplete = session.HistoryRead
	for _, interval := range session.HistoryCovered {
		result.HistoryCovered = append(result.HistoryCovered, HistoryRange{Start: interval.Start, End: interval.End})
	}
	result.Symbols = Table{session.Symbols.Status, session.Symbols.Count}
	result.Groups = Table{session.Groups.Status, session.Groups.Count}
	result.Optional = Table{session.Auxiliary.Status, session.Auxiliary.Count}
}

func trades(values []protocol.TradeInfo) []Trade {
	result := make([]Trade, 0, len(values))
	for _, v := range values {
		result = append(result, Trade{
			Ticket: v.Ticket, Symbol: v.Symbol, Digits: v.Digits, Command: v.Command,
			VolumeHundredths: v.VolumeHundredths, Time20: v.Time20, Time40: v.Time40,
			OpenTime: v.Time20, CloseTime: v.Time40, OpenPrice: v.OpenPrice, ClosePrice: v.ClosePrice,
			SL: v.Sl, TP: v.Tp, Commission: v.Commission, Swap: v.Swap, Profit: v.Profit, Taxes: v.Taxes,
		})
	}
	return result
}
