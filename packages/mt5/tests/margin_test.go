package mt5_test

import (
	"encoding/binary"
	"math"
	"testing"

	"portexa/mt5/internal/protocol"
)

func TestSyncComputesCFDLeverageMargin(t *testing.T) {
	const login = uint64(10001001)
	for _, tc := range []struct {
		name   string
		change func(catalog, symbol, trades []byte)
		want   float64
	}{
		{name: "basic hedge", want: 46.67},
		{name: "larger leg", want: 60, change: func(_, symbol, _ []byte) {
			binary.LittleEndian.PutUint32(symbol[1952+1080:], 4)
		}},
		{name: "no full catalog", change: func(catalog, _, _ []byte) {
			binary.LittleEndian.PutUint32(catalog[7357:], 1)
		}},
		{name: "fixed maintenance margin", change: func(_, symbol, _ []byte) {
			putMarginFloat(symbol[1952:], 844, 100)
		}},
		{name: "pending orders", change: func(_, _, trades []byte) {
			binary.LittleEndian.PutUint32(trades[5:], 1)
		}},
		{name: "incomplete position list", change: func(_, _, trades []byte) {
			binary.LittleEndian.PutUint32(trades[14:], 3)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const catalogHeaderSize, symbolSize, positionSize = 7365, 3716, 672
			const accountTag = catalogHeaderSize + symbolSize
			const tradesStart = accountTag + 1 + protocol.AccountRecordSize
			// Leave space for sections that follow the counted positions.
			payload := make([]byte, tradesStart+18+2*positionSize+80)
			payload[4] = 0x07
			binary.LittleEndian.PutUint32(payload[7361:], 1)
			symbol := payload[catalogHeaderSize:accountTag]
			copy(symbol[8:72], protocol.EncodeUTF16LE("TESTUSD"))
			putMarginFloat(symbol, 1648, 10)
			binary.LittleEndian.PutUint32(symbol[1660:], 4)
			group := symbol[1952:]
			putMarginFloat(group, 916, 1.5)
			putMarginFloat(group, 924, 1)
			putMarginFloat(group, 988, 5)

			payload[accountTag] = protocol.AccountRecordTag
			account := payload[accountTag+1 : tradesStart]
			binary.LittleEndian.PutUint64(account, login)
			binary.LittleEndian.PutUint32(account[456:], protocol.InvestorFlag)
			binary.LittleEndian.PutUint32(account[2020:], 100)

			trades := payload[tradesStart:]
			trades[0], trades[9] = 0x1f, 0x24
			binary.LittleEndian.PutUint32(trades[14:], 2)
			for side := range 2 {
				row := trades[18+side*positionSize:]
				binary.LittleEndian.PutUint64(row[72:], login)
				copy(row[120:184], protocol.EncodeUTF16LE("TESTUSD"))
				binary.LittleEndian.PutUint32(row[184:], uint32(side))
				putMarginFloat(row, 192, float64(100+side*100))
				binary.LittleEndian.PutUint64(row[224:], uint64(2-side)*100_000_000)
				putMarginFloat(row, 248, 2)
				binary.LittleEndian.PutUint64(row[288:], uint64(10+side))
				putMarginFloat(row, 360, 10)
				binary.LittleEndian.PutUint32(row[372:], 2)
			}
			if tc.change != nil {
				tc.change(payload[:catalogHeaderSize], symbol, trades)
			}
			result, err := protocol.ParseSyncPayload(payload, login)
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Account; got.MarginAvailable != (tc.want > 0) || got.Margin != tc.want {
				t.Fatalf("margin = %v (available: %v), want %v", got.Margin, got.MarginAvailable, tc.want)
			}
		})
	}
}

func putMarginFloat(row []byte, offset int, value float64) {
	binary.LittleEndian.PutUint64(row[offset:], math.Float64bits(value))
}
