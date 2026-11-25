package protocol

import (
	"encoding/binary"
	"io"
	"math"
)

const (
	TypeHistory        = 0x65
	dealHistoryCommand = 0x21
	DealRecordSize     = 672
)

type DealRecord struct {
	Ticket, Order, Position uint64
	Time                    int64 // Broker wall clock, in Unix milliseconds.
	Type, Entry, Digits     uint32
	VolumeUnits             uint64
	Symbol, Comment         string
	Price, SL, TP           float64
	Profit, Commission      float64
	Swap, Fee               float64
}

// The wire interval includes both endpoints. Send no cached month descriptors.
func BuildHistoryRequest(start, end uint32) []byte {
	request := make([]byte, 21)
	request[0] = dealHistoryCommand
	binary.LittleEndian.PutUint64(request[1:9], uint64(start))
	binary.LittleEndian.PutUint64(request[9:17], uint64(end-1))
	return request
}

func FetchHistoryDeals(conn io.ReadWriter, outState, inState *RollingPayload, seq uint16, login uint64, start, end uint32) ([]DealRecord, error) {
	request := outState.Encode(BuildHistoryRequest(start, end))
	if err := WriteFrame(conn, FrameHeader{Type: TypeHistory, Length: uint32(len(request)), Sequence: seq, Flags: FlagFinal}, request); err != nil {
		return nil, err
	}

	return parseHistoryReply(&historyReplyReader{conn: conn, state: inState, sequence: seq}, login, start, end)
}

func ParseDealRecord(row []byte, login uint64) (DealRecord, error) {
	if len(row) != DealRecordSize || binary.LittleEndian.Uint64(row[72:80]) != login {
		return DealRecord{}, ErrProtocolMismatch
	}
	uint64At := func(offset int) uint64 {
		return binary.LittleEndian.Uint64(row[offset : offset+8])
	}
	float64At := func(offset int) float64 {
		return math.Float64frombits(uint64At(offset))
	}
	ticket := uint64At(0)
	seconds := uint64At(104)
	millis := uint64At(548)
	if seconds == 0 || seconds > math.MaxInt32 || ticket == 0 {
		return DealRecord{}, ErrProtocolMismatch
	}
	if millis == 0 {
		millis = seconds * 1000
	}
	if millis/1000 != seconds {
		return DealRecord{}, ErrProtocolMismatch
	}
	return DealRecord{
		Ticket: ticket, Order: uint64At(88), Position: uint64At(288), Time: int64(millis),
		Type: binary.LittleEndian.Uint32(row[184:188]), Entry: binary.LittleEndian.Uint32(row[188:192]),
		Digits: binary.LittleEndian.Uint32(row[368:372]), VolumeUnits: uint64At(224),
		Symbol: decodeUTF16String(row[120:184]), Comment: decodeUTF16String(row[296:360]),
		Price: float64At(200), SL: float64At(208), TP: float64At(216), Profit: float64At(232),
		Commission: float64At(256), Fee: float64At(264), Swap: float64At(272),
	}, nil
}
