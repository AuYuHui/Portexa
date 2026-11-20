package protocol

import (
	"encoding/binary"
	"io"
	"math"
	"time"
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

	var payload []byte
	deadliner, hasDeadline := conn.(interface{ SetReadDeadline(time.Time) error })
	for {
		if hasDeadline {
			if err := deadliner.SetReadDeadline(time.Now().Add(4 * time.Second)); err != nil {
				return nil, err
			}
		}
		header, err := ReadHeader(conn)
		if err != nil {
			return nil, err
		}
		raw, err := ReadFramePayload(conn, header.Length)
		if err != nil {
			return nil, err
		}
		compressed := header.Flags&FlagCompressed != 0
		piece := inState.Decode(raw, compressed)
		// Account updates share the rolling state but are not query replies.
		if header.Type != TypeHistory || header.Sequence != seq {
			continue
		}
		if compressed {
			piece, err = DecompressLZO(piece)
			if err != nil {
				return nil, err
			}
		}
		if len(piece) > MaxFrameSize-len(payload) {
			return nil, ErrFrameTooLarge
		}
		payload = append(payload, piece...)
		if header.Flags&FlagFinal != 0 {
			return parseHistoryReply(payload, login, start, end)
		}
	}
}

func parseHistoryReply(payload []byte, login uint64, start, end uint32) ([]DealRecord, error) {
	const headerSize = 9
	const monthHeaderSize = 12
	if len(payload) < headerSize || payload[0] != dealHistoryCommand {
		return nil, ErrProtocolMismatch
	}
	groups := binary.LittleEndian.Uint32(payload[5:9])
	payload = payload[headerSize:]
	if groups > uint32(len(payload)/monthHeaderSize) {
		return nil, ErrProtocolMismatch
	}
	var deals []DealRecord
	seen := make(map[uint64]struct{})
	for range groups {
		if len(payload) < monthHeaderSize+4 {
			return nil, ErrProtocolMismatch
		}
		mode := binary.LittleEndian.Uint32(payload[8:12])
		payload = payload[monthHeaderSize:]
		var tickets map[uint64]struct{}
		var footerSize int
		switch mode {
		case 0:
			// The manifest lists every ticket. With no client cache,
			// the reply must include a record for each listed ticket.
			count := binary.LittleEndian.Uint32(payload[:4])
			payload = payload[4:]
			footerSize = 16
			if len(payload) < footerSize+4 || count > uint32((len(payload)-footerSize-4)/(8+DealRecordSize)) {
				return nil, ErrTruncatedFrame
			}
			tickets = make(map[uint64]struct{}, count)
			for range count {
				tickets[binary.LittleEndian.Uint64(payload[:8])] = struct{}{}
				payload = payload[8:]
			}
			if len(tickets) != int(count) {
				return nil, ErrProtocolMismatch
			}
		case 2, 3, 4:
			// Full records without a ticket manifest.
		default:
			return nil, ErrProtocolMismatch
		}
		rows := binary.LittleEndian.Uint32(payload[:4])
		payload = payload[4:]
		if len(payload) < footerSize || rows > uint32((len(payload)-footerSize)/DealRecordSize) {
			return nil, ErrTruncatedFrame
		}
		if tickets != nil && len(tickets) != int(rows) {
			return nil, ErrProtocolMismatch
		}
		for range rows {
			deal, err := ParseDealRecord(payload[:DealRecordSize], login)
			payload = payload[DealRecordSize:]
			if err != nil {
				return nil, err
			}
			if tickets != nil {
				if _, present := tickets[deal.Ticket]; !present {
					return nil, ErrProtocolMismatch
				}
				delete(tickets, deal.Ticket)
			}
			if deal.Time < int64(start)*1000 || deal.Time >= int64(end)*1000 {
				continue
			}
			if _, duplicate := seen[deal.Ticket]; duplicate {
				return nil, ErrProtocolMismatch
			}
			seen[deal.Ticket] = struct{}{}
			deals = append(deals, deal)
		}
		payload = payload[footerSize:]
	}
	if len(payload) != 0 {
		return nil, ErrProtocolMismatch
	}
	return deals, nil
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
