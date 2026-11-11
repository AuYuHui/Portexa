package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math"
)

// Offsets are relative to a decoded 224-byte TradeRecord. Time fields keep
// their wire offsets until open/close semantics are independently checked.
type TradeInfo struct {
	Ticket                          int32
	Symbol                          string
	Comment                         string
	Digits, Command                 int32
	VolumeHundredths                int32
	Time20, Time40                  uint32
	OpenPrice, ClosePrice           float64
	MarginRate                      float64
	Sl, Tp                          float64
	Commission, Swap, Profit, Taxes float64
}

func ParseTradeRecords(data []byte) ([]TradeInfo, error) {
	if len(data)%TradeRecordSize != 0 {
		return nil, errors.New("trade Record array has a partial final Record")
	}
	result := make([]TradeInfo, 0, len(data)/TradeRecordSize)
	for len(data) > 0 {
		item := data[:TradeRecordSize]
		value := TradeInfo{
			Ticket:           int32(binary.LittleEndian.Uint32(item)),
			Symbol:           string(item[8:20]),
			Comment:          tradeText(item[0x94:0xb4]),
			Digits:           int32(binary.LittleEndian.Uint32(item[0x14:])),
			Command:          int32(binary.LittleEndian.Uint32(item[0x18:])),
			VolumeHundredths: int32(binary.LittleEndian.Uint32(item[0x1c:])),
			Time20:           binary.LittleEndian.Uint32(item[0x20:]),
			Time40:           binary.LittleEndian.Uint32(item[0x40:]),
		}
		if value.Command < 0 || value.Command > 7 {
			return nil, ErrUnsupportedProtocol
		}
		for i, b := range []byte(value.Symbol) {
			if b == 0 {
				value.Symbol = value.Symbol[:i]
				break
			}
		}
		if value.Digits < 0 || value.Digits > 16 {
			return nil, errors.New("trade price precision is outside the supported range")
		}
		if value.Command <= 5 && (value.Symbol == "" || value.VolumeHundredths <= 0 || value.Time20 > math.MaxInt32 || value.Time40 > math.MaxInt32) {
			return nil, errors.New("trade has an invalid symbol, volume or timestamp")
		}
		for offset, target := range map[int]*float64{
			0x28: &value.OpenPrice, 0x30: &value.Sl, 0x38: &value.Tp, 0x78: &value.ClosePrice,
			0x60: &value.Commission, 0x70: &value.Swap,
			0x80: &value.Profit, 0x88: &value.Taxes,
			0xc0: &value.MarginRate,
		} {
			*target = math.Float64frombits(binary.LittleEndian.Uint64(item[offset:]))
			if math.IsNaN(*target) || math.IsInf(*target, 0) {
				return nil, errors.New("trade price or money field is not finite")
			}
		}
		result = append(result, value)
		data = data[TradeRecordSize:]
	}
	return result, nil
}

func tradeText(value []byte) string {
	if end := bytes.IndexByte(value, 0); end >= 0 {
		value = value[:end]
	}
	return string(value)
}
