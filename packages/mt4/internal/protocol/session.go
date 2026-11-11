package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"unicode/utf16"
)

const (
	SyncLimit       = 16 << 20
	AccountBaseSize = 0x3600
	TradeRecordSize = 224
)

type TableResult struct {
	Status  byte
	Count   int
	Records []TradeInfo
	Data    []byte
}

type AccountInfo struct {
	Leverage          uint32
	Balance, Credit   float64
	InvestorIndicator uint32
	DemoIndicator     uint32
	TradeRecords      int
	TrailingBytes     int
	Raw               []byte
	Orders            []TradeInfo
	Currency          string
	Equity            float64
	Margin            float64
	MarginAvailable   bool
}

type SessionResult struct {
	Configuration   ServerConfiguration
	Symbols, Groups TableResult
	Auxiliary       TableResult
	Account         AccountInfo
	History         TableResult
	HistoryRead     bool
	AccountRead     bool
	HistoryCovered  []HistoryInterval
}

func WriteCommand(writer io.Writer, state *StreamState, command []byte) error {
	if err := validateReadOnlyCommand(command); err != nil {
		return err
	}
	value := bytes.Clone(command)
	state.Encode(value)
	if _, err := io.Copy(writer, bytes.NewReader(value)); err != nil {
		return fmt.Errorf("session command write failed: %w", err)
	}
	return nil
}

func ReadTable(reader io.Reader, state *StreamState, stride int) (TableResult, error) {
	return ReadRecordBlock(reader, state, stride, false)
}

func ReadRecordBlock(reader io.Reader, state *StreamState, stride int, allowEmpty bool) (TableResult, error) {
	var result TableResult
	status, err := state.Read(reader, 1)
	if err != nil {
		return result, err
	}
	result.Status = status[0]
	if result.Status == 1 {
		return result, nil // No body: unchanged for config tables, empty for history.
	}
	if result.Status != 0 {
		return result, &StatusError{Status: result.Status}
	}
	data, err := ReadCompressed(reader, state, SyncLimit)
	if err != nil {
		return result, err
	}
	if (!allowEmpty && len(data) == 0) || len(data)%stride != 0 {
		return result, errors.New("synchronized table has an invalid Record boundary")
	}
	result.Count = len(data) / stride
	if stride == 1936 {
		result.Data = data
	}
	if stride == TradeRecordSize {
		result.Records, err = ParseTradeRecords(data)
		if err != nil {
			return result, err
		}
	}
	return result, nil
}

func ReadAuxiliary(reader io.Reader, state *StreamState) (TableResult, error) {
	var result TableResult
	status, err := state.Read(reader, 1)
	if err != nil {
		return result, err
	}
	result.Status = status[0]
	if result.Status != 0 {
		return result, nil // The reference caller also proceeds without this optional table.
	}
	value, err := state.Read(reader, 4)
	if err != nil {
		return result, err
	}
	count := int(binary.LittleEndian.Uint32(value))
	if count < 1 || count > 1024 {
		return result, errors.New("optional table count is outside the verified range")
	}
	if _, err := state.Read(reader, count*68); err != nil {
		return result, err
	}
	result.Count = count
	return result, nil
}

func AccountRequest(serverBuild uint16) []byte {
	value := binary.LittleEndian.AppendUint32([]byte{0x1e}, 2) // Single connection, with news disabled.
	prefix := make([]byte, 72)
	binary.LittleEndian.PutUint32(prefix[8:], 2)
	value = append(value, prefix...)
	if serverBuild < 1362 {
		return value
	}
	// Public application identifiers of the analyzed mobile protocol profile.
	// These are compatibility fields, not a signature or a hardware identity.
	// No user's computer name or private installation metadata is included.
	metadata := "package=4LK8F7J843.net.metaquotes.MetaTrader4Terminal\tdevid=4LK8F7J843\tfile=MetaTrader4Terminal\tversion=1478\tos_ver=iOS 18.0\t"
	var body []byte
	for _, word := range utf16.Encode([]rune(metadata)) {
		body = binary.LittleEndian.AppendUint16(body, word)
	}
	body = append(body, 0, 0)
	outer := make([]byte, 8)
	binary.LittleEndian.PutUint16(outer, uint16(len(body)+8))
	binary.LittleEndian.PutUint16(outer[2:], 2)
	inner := make([]byte, 8)
	binary.LittleEndian.PutUint16(inner, uint16(len(body)))
	binary.LittleEndian.PutUint16(inner[2:], 13)
	value = append(value, outer...)
	value = append(value, inner...)
	return append(value, body...)
}

func ParseAccount(data []byte) (AccountInfo, error) {
	var result AccountInfo
	if len(data) < AccountBaseSize {
		return result, errors.New("account response is shorter than the verified base structure")
	}
	result.DemoIndicator = binary.LittleEndian.Uint32(data)
	result.Leverage = binary.LittleEndian.Uint32(data[0x44:])
	result.Balance = math.Float64frombits(binary.LittleEndian.Uint64(data[0x48:]))
	result.Credit = math.Float64frombits(binary.LittleEndian.Uint64(data[0x50:]))
	result.InvestorIndicator = binary.LittleEndian.Uint32(data[0x35d8:])
	if math.IsNaN(result.Balance) || math.IsInf(result.Balance, 0) || math.IsNaN(result.Credit) || math.IsInf(result.Credit, 0) {
		return result, errors.New("account money fields are not finite")
	}
	result.TradeRecords = (len(data) - AccountBaseSize) / TradeRecordSize
	result.TrailingBytes = (len(data) - AccountBaseSize) % TradeRecordSize
	if result.TrailingBytes != 0 {
		return result, errors.New("account response contains a partial trade record")
	}
	var err error
	result.Orders, err = ParseTradeRecords(data[AccountBaseSize : len(data)-result.TrailingBytes])
	if err != nil {
		return result, err
	}
	seen := make(map[int32]bool, len(result.Orders))
	for _, order := range result.Orders {
		if order.Ticket <= 0 || seen[order.Ticket] {
			return result, errors.New("account has an invalid or repeated order ticket")
		}
		seen[order.Ticket] = true
	}
	result.Raw = data
	// Group config is copied from wire+0x58 to AccountInfoBase+0x278.
	// The official trade UI's currency pointer is AccountInfoBase+0x3714.
	currency := data[0x34f4:0x3500]
	if end := bytes.IndexByte(currency, 0); end >= 0 {
		currency = currency[:end]
	} else {
		return result, errors.New("account currency is not terminated")
	}
	for _, ch := range currency {
		if ch < 0x21 || ch > 0x7e {
			return result, errors.New("account currency has an invalid character")
		}
	}
	result.Currency = string(currency)
	if result.Currency == "" {
		return result, errors.New("account currency is empty")
	}
	result.Equity = result.Balance + result.Credit
	for _, order := range result.Orders {
		if order.Command == 0 || order.Command == 1 {
			result.Equity += order.Profit + order.Commission + order.Swap + order.Taxes
		}
	}
	if math.IsInf(result.Equity, 0) || math.IsNaN(result.Equity) {
		return result, errors.New("calculated equity is not finite")
	}
	return result, nil
}

func ReadAccount(reader io.Reader, state *StreamState) (AccountInfo, error) {
	status, err := state.Read(reader, 1)
	if err != nil {
		return AccountInfo{}, fmt.Errorf("account status read: %w", err)
	}
	if status[0] != 0 {
		return AccountInfo{}, &StatusError{Status: status[0]}
	}
	data, err := ReadCompressed(reader, state, SyncLimit)
	if err != nil {
		return AccountInfo{}, fmt.Errorf("account block read: %w", err)
	}
	return ParseAccount(data)
}

func HistoryRequest(start, end uint32) ([]byte, error) {
	if start >= end || end > math.MaxInt32 {
		return nil, errors.New("history interval must be a valid increasing Unix time range")
	}
	request := binary.LittleEndian.AppendUint32([]byte{0x22}, start)
	return binary.LittleEndian.AppendUint32(request, end), nil
}

func ReadHistory(reader io.Reader, state *StreamState) (TableResult, error) {
	// Unlike digest-based config queries, 0x22 carries only a time interval.
	// The reference caller assigns a newly created empty array on status 1.
	return ReadRecordBlock(reader, state, TradeRecordSize, true)
}

func ReadLiveAccount(conn io.ReadWriter, response *InitialResponse, historyStart, historyEnd, historyWindow uint32) (SessionResult, error) {
	var result SessionResult
	var err error
	for _, command := range []byte{0x08, 0x0a} {
		request := make([]byte, 17) // No local table cache: the digest is all zero.
		request[0] = command
		if err := WriteCommand(conn, &response.Tx, request); err != nil {
			return result, operationError("tables", err)
		}
		stride := 1936
		if command == 0x0a {
			stride = 80
		}
		table, err := ReadTable(conn, &response.Rx, stride)
		if err != nil {
			return result, operationError("tables", err)
		}
		if command == 0x08 {
			result.Symbols = table
		} else {
			result.Groups = table
		}
	}
	if err := WriteCommand(conn, &response.Tx, []byte{0x21}); err != nil {
		return result, operationError("auxiliary", err)
	}
	result.Auxiliary, err = ReadAuxiliary(conn, &response.Rx)
	if err != nil {
		return result, operationError("auxiliary", err)
	}
	if historyStart < historyEnd {
		result.History, result.HistoryCovered, err = synchronizeHistory(conn, response, historyStart, historyEnd, historyWindow)
		if err != nil {
			return result, operationError("history", err)
		}
		result.HistoryRead = true
	}
	if err := WriteCommand(conn, &response.Tx, AccountRequest(response.ServerBuild)); err != nil {
		return result, operationError("account", err)
	}
	result.Account, err = ReadAccount(conn, &response.Rx)
	if err != nil {
		return result, operationError("account", err)
	}
	result.AccountRead = true
	if err := CalculateMargin(&result.Account, result.Symbols.Data); err != nil {
		return result, operationError("margin", err)
	}
	return result, nil
}
