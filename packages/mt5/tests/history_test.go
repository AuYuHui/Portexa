package mt5_test

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"portexa/mt5/internal/protocol"
)

func historyRequest(start, end uint32) []byte {
	request := make([]byte, 21)
	request[0] = 0x21
	binary.LittleEndian.PutUint64(request[1:9], uint64(start))
	binary.LittleEndian.PutUint64(request[9:17], uint64(end-1))
	return request
}

func historyTrade(login uint64, millis int64) []byte {
	row := make([]byte, 672)
	for offset, value := range map[int]uint64{
		0: 2001, 72: login, 88: 1001, 104: uint64(millis / 1000), 224: 1234567, 288: 3001, 548: uint64(millis),
	} {
		binary.LittleEndian.PutUint64(row[offset:offset+8], value)
	}
	for offset, value := range map[int]float64{200: 2650.50, 232: 15.25, 256: -1, 264: -0.25, 272: -2} {
		binary.LittleEndian.PutUint64(row[offset:offset+8], math.Float64bits(value))
	}
	binary.LittleEndian.PutUint32(row[184:188], 1)
	binary.LittleEndian.PutUint32(row[188:192], 3)
	binary.LittleEndian.PutUint32(row[368:372], 3)
	copy(row[120:184], protocol.EncodeUTF16LE("TESTUSD"))
	return row
}

func historyBalance(login uint64, millis int64) []byte {
	row := make([]byte, 672)
	binary.LittleEndian.PutUint64(row[0:8], 2002)
	binary.LittleEndian.PutUint64(row[72:80], login)
	binary.LittleEndian.PutUint64(row[104:112], uint64(millis/1000))
	binary.LittleEndian.PutUint64(row[232:240], math.Float64bits(50))
	binary.LittleEndian.PutUint32(row[184:188], 2)
	binary.LittleEndian.PutUint64(row[548:556], uint64(millis))
	copy(row[296:360], protocol.EncodeUTF16LE("deposit"))
	return row
}

func historyReply(rows []byte) []byte {
	reply := make([]byte, 25)
	reply[0] = 0x21
	binary.LittleEndian.PutUint32(reply[5:9], 1)
	binary.LittleEndian.PutUint32(reply[17:21], 2) // Full month records.
	binary.LittleEndian.PutUint32(reply[21:25], uint32(len(rows)/672))
	return append(reply, rows...)
}

func historyReplyWithManifest(row []byte) []byte {
	reply := historyReply(nil)[:21]
	binary.LittleEndian.PutUint32(reply[17:21], 0)
	reply = binary.LittleEndian.AppendUint32(reply, 1) // Manifest ticket count.
	reply = append(reply, row[:8]...)
	reply = binary.LittleEndian.AppendUint32(reply, 1) // Deal count.
	reply = append(reply, row...)
	return append(reply, make([]byte, 16)...)
}

func TestHistoryRequiresCompleteReplyForRequestedSequence(t *testing.T) {
	const login, seq, start, end = 12345, 4, 1000, 2000
	row := historyTrade(login, start*1000+123)
	cacheReply := historyReply(nil)[:21]
	cacheReply[17] = 1
	manifestReply := historyReplyWithManifest(row)
	missingTicket := bytes.Clone(manifestReply)
	binary.LittleEndian.PutUint64(missingTicket[25:33], 123)
	for _, tt := range []struct {
		name    string
		kind    uint8
		seq     uint16
		flags   uint16
		payload []byte
		want    int
		fail    bool
	}{
		{name: "EOF before response", fail: true},
		{name: "notification only", kind: 0x37, seq: seq, flags: protocol.FlagFinal, payload: row, fail: true},
		{name: "wrong sequence", kind: protocol.TypeHistory, seq: seq + 1, flags: protocol.FlagFinal, payload: historyReply(row), fail: true},
		{name: "missing final", kind: protocol.TypeHistory, seq: seq, payload: historyReply(row), fail: true},
		{name: "truncated record", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: historyReply(row)[:28], fail: true},
		{name: "invalid compression", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal | protocol.FlagCompressed, payload: []byte{0}, fail: true},
		{name: "cache-only response", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: cacheReply, fail: true},
		{name: "different login", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: historyReply(historyTrade(login+1, start*1000)), fail: true},
		{name: "complete empty", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: historyReply(nil)},
		{name: "exclusive end", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: historyReply(historyTrade(login, end*1000))},
		{name: "complete trade", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: historyReply(row), want: 1},
		{name: "complete manifest", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: manifestReply, want: 1},
		{name: "unmatched manifest ticket", kind: protocol.TypeHistory, seq: seq, flags: protocol.FlagFinal, payload: missingTicket, fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var table [256]byte
			var inbound, outbound bytes.Buffer
			if tt.kind != 0 {
				encoded := protocol.NewRollingPayload(table).Encode(tt.payload)
				err := protocol.WriteFrame(&inbound, protocol.FrameHeader{
					Type: tt.kind, Sequence: tt.seq, Flags: tt.flags, Length: uint32(len(encoded)),
				}, encoded)
				if err != nil {
					t.Fatal(err)
				}
			}
			stream := struct {
				io.Reader
				io.Writer
			}{&inbound, &outbound}
			deals, err := protocol.FetchHistoryDeals(stream, protocol.NewRollingPayload(table),
				protocol.NewRollingPayload(table), seq, login, start, end)
			if (err != nil) != tt.fail || len(deals) != tt.want {
				t.Fatalf("got %d deals and error %v", len(deals), err)
			}
		})
	}
}
