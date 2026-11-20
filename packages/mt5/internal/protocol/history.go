package protocol

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"time"
)

const (
	TypeHistoryRequest = 101 // 0x65
	TypeHistoryReply   = 55  // 0x37

	DealRecordTag  = 0x21
	DealRecordSize = 1765

	// Windows FILETIME epoch offset in 100ns (from 1601-01-01 to 1970-01-01)
	FiletimeEpochOffset = 116444736000000000
)

type DealRecord struct {
	Ticket     uint64
	Order      uint64
	Position   uint64
	Time       int64 // Unix millisecond
	Type       int   // 0 buy, 1 sell, 2 balance, 3 credit...
	Entry      int   // 0 in, 1 out, 2 in/out, 3 out by
	Symbol     string
	Lots       float64
	Price      float64
	Profit     float64
	Commission float64
	Swap       float64
	Comment    string
}

func UnixToFILETIME(t time.Time) uint64 {
	nanos := t.UnixNano()
	return uint64(nanos/100) + FiletimeEpochOffset
}

func FILETIMEToUnixMilli(ft uint64) int64 {
	if ft < FiletimeEpochOffset {
		return 0
	}
	return int64((ft - FiletimeEpochOffset) / 10000)
}

func BuildHistoryRequest(subCmd uint8, startTime, endTime time.Time) []byte {
	buf := make([]byte, 57)
	buf[0] = subCmd
	buf[1] = 0x00

	prefix := []byte{
		0x91, 0x13, 0x6a, 0x00, 0x00, 0x00, 0x00, 0xff,
		0xde, 0x00, 0x6b, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x00, 0x00, 0x00, 0x00, 0x81, 0xe6, 0x6a, 0x00,
		0x00, 0x00, 0x00, 0xff, 0x0d, 0x0e, 0x6b, 0x00,
		0x00, 0x00, 0x00,
	}
	copy(buf[2:37], prefix)

	endFT := UnixToFILETIME(endTime)
	binary.LittleEndian.PutUint64(buf[37:45], endFT)

	if subCmd == 0x20 {
		buf[45] = 0x2e
	} else {
		buf[45] = 0x2f
	}
	buf[46] = 0x11
	return buf
}

func FetchHistoryDeals(r io.Reader, w io.Writer, outState, inState *RollingPayload, startSeq uint16, start, end time.Time) ([]DealRecord, error) {
	// Send 0x65 sub-command 0x20
	req20 := BuildHistoryRequest(0x20, start, end)
	enc20 := outState.Encode(req20)
	if err := WriteFrame(w, FrameHeader{Type: TypeHistoryRequest, Length: uint32(len(enc20)), Sequence: startSeq, Flags: FlagFinal}, enc20); err != nil {
		return nil, err
	}

	// Send 0x65 sub-command 0x21
	req21 := BuildHistoryRequest(0x21, start, end)
	enc21 := outState.Encode(req21)
	if err := WriteFrame(w, FrameHeader{Type: TypeHistoryRequest, Length: uint32(len(enc21)), Sequence: startSeq + 1, Flags: FlagFinal}, enc21); err != nil {
		return nil, err
	}

	type deadliner interface {
		SetReadDeadline(time.Time) error
	}
	dl, hasDeadline := r.(deadliner)
	if hasDeadline {
		_ = dl.SetReadDeadline(time.Now().Add(3 * time.Second))
	}

	var deals []DealRecord
	// Read replies until stream quiesces or final flags received
	for {
		hdr, err := ReadHeader(r)
		if err != nil {
			if errorsIsEOF(err) || isTimeout(err) {
				break
			}
			return deals, err
		}
		if hasDeadline {
			_ = dl.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		}

		rawPayload, err := ReadFramePayload(r, hdr.Length)
		if err != nil {
			if errorsIsEOF(err) || isTimeout(err) {
				break
			}
			return deals, err
		}

		compressed := (hdr.Flags & FlagCompressed) != 0
		decPayload := inState.Decode(rawPayload, compressed)

		piece := decPayload
		if compressed {
			piece, err = DecompressLZO(decPayload)
			if err != nil {
				continue
			}
		}

		if hdr.Type == TypeHistoryReply && len(piece) >= DealRecordSize && piece[0] == DealRecordTag {
			deal := ParseDealRecord(piece)
			if deal.Ticket > 0 {
				deals = append(deals, deal)
			}
		}
	}

	return deals, nil
}

func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	type timeoutErr interface {
		Timeout() bool
	}
	if te, ok := err.(timeoutErr); ok && te.Timeout() {
		return true
	}
	return errors.Is(err, os.ErrDeadlineExceeded)
}

func ParseDealRecord(b []byte) DealRecord {
	if len(b) < DealRecordSize || b[0] != DealRecordTag {
		return DealRecord{}
	}

	sub := binary.LittleEndian.Uint32(b[9:13])
	ticket := uint64(binary.LittleEndian.Uint32(b[5:9]))
	if ticket == 0 || sub == 0 {
		ticket = binary.LittleEndian.Uint64(b[592:600])
		if ticket == 0 {
			ticket = binary.LittleEndian.Uint64(b[4:12])
		}
	}
	if ticket == 0 {
		return DealRecord{}
	}

	timeFT := binary.LittleEndian.Uint64(b[909:917])
	if timeFT < FiletimeEpochOffset {
		timeFT = binary.LittleEndian.Uint64(b[616:624])
	}
	milli := FILETIMEToUnixMilli(timeFT)
	if milli == 0 {
		milli = time.Now().UnixMilli()
	}

	openPrice := math.Float64frombits(binary.LittleEndian.Uint64(b[1021:1029]))
	closePrice := math.Float64frombits(binary.LittleEndian.Uint64(b[1029:1037]))
	profit := math.Float64frombits(binary.LittleEndian.Uint64(b[1061:1069]))
	volumeUnits := binary.LittleEndian.Uint64(b[1053:1061])
	lots := float64(volumeUnits) / 100000000.0

	price := closePrice
	if price == 0 {
		price = openPrice
	}
	if price == 0 {
		price = math.Float64frombits(binary.LittleEndian.Uint64(b[272:280]))
	}
	if profit == 0 {
		profit = math.Float64frombits(binary.LittleEndian.Uint64(b[640:648]))
	}
	if lots == 0 {
		rawLots := binary.LittleEndian.Uint64(b[632:640])
		if rawLots > 0 {
			lots = float64(rawLots) / 10000.0
		}
	}

	var symbol string
	if len(b) >= 949+32 {
		symbol = decodeUTF16String(b[949 : 949+32])
	}
	var comment string
	if len(b) >= 1125+32 {
		comment = decodeUTF16String(b[1125 : 1125+32])
	}

	order := binary.LittleEndian.Uint64(b[600:608])
	pos := binary.LittleEndian.Uint64(b[608:616])

	return DealRecord{
		Ticket:     ticket,
		Order:      order,
		Position:   pos,
		Time:       milli,
		Price:      price,
		Profit:     profit,
		Lots:       lots,
		Symbol:     symbol,
		Comment:    comment,
		Entry:      1,
	}
}

func errorsIsEOF(err error) bool {
	return err == io.EOF || err == ErrPeerClosed
}
