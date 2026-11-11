package protocol_test

import (
	"encoding/binary"
	"math"
	. "portexa/mt4/internal/protocol"
	"testing"
)

func TestTradeRecordLayoutAndBoundary(t *testing.T) {
	data := make([]byte, TradeRecordSize)
	binary.LittleEndian.PutUint32(data, 42)
	copy(data[8:20], "TEST.USD")
	binary.LittleEndian.PutUint32(data[0x14:], 5)
	binary.LittleEndian.PutUint32(data[0x18:], 3)
	binary.LittleEndian.PutUint32(data[0x1c:], 125)
	binary.LittleEndian.PutUint32(data[0x20:], 1700000000)
	binary.LittleEndian.PutUint32(data[0x40:], 1700003600)
	copy(data[0x94:0xb4], "Deposit\x00ignored")
	for offset, value := range map[int]float64{0x30: 1.125, 0x38: 1.25, 0x60: -1.5, 0x70: -0.125, 0x80: 12.5, 0x88: 0.25} {
		binary.LittleEndian.PutUint64(data[offset:], math.Float64bits(value))
	}
	got, err := ParseTradeRecords(data)
	if err != nil || len(got) != 1 {
		t.Fatalf("synthetic trade Record failed: %v", err)
	}
	r := got[0]
	if r.Ticket != 42 || r.Symbol != "TEST.USD" || r.Comment != "Deposit" || r.Command != 3 || r.Digits != 5 || r.VolumeHundredths != 125 || r.Time20 != 1700000000 || r.Time40 != 1700003600 || r.Commission != -1.5 || r.Swap != -0.125 || r.Profit != 12.5 || r.Taxes != 0.25 {
		t.Fatal("wire fields were not preserved")
	}
	if _, err := ParseTradeRecords(data[:TradeRecordSize-1]); err == nil {
		t.Fatal("partial final trade Record was accepted")
	}
	binary.LittleEndian.PutUint64(data[0x80:], math.Float64bits(math.NaN()))
	if _, err := ParseTradeRecords(data); err == nil {
		t.Fatal("nonfinite trade profit was accepted")
	}
}
