package terminal

import (
	"math"
	"unicode/utf16"
)

type Account struct {
	Server   string
	Currency string
	Balance  float64
	Credit   float64
	Margin   float64
	Leverage uint32
	Investor bool
	investor bool
}

func (a Account) IsInvestor() bool {
	return a.Investor || a.investor
}

type Position struct {
	ID                              uint64
	Symbol                          string
	Side, Digits, OpenTime          uint32
	VolumeUnits                     uint64 // 1 lot = 100,000,000 units
	OpenPrice, CurrentPrice, SL, TP float64
	Profit, Commission, Swap        float64
}

type PendingOrder struct {
	Ticket                            uint64
	Symbol                            string
	Type, Digits, SetupTime           uint32
	RemainingVolumeUnits              uint64
	Price, TriggerPrice, CurrentPrice float64
	SL, TP                            float64
}

type Result struct {
	Account   Account
	Positions []Position
	Orders    []PendingOrder
	deals     []Deal
}

func decodeAccount(b []byte) (Account, error) {
	const accountSize = 816
	if len(b) < accountSize {
		return Account{}, ErrProtocol
	}
	isInvestor := le.Uint32(b[1:5])&8 != 0
	return Account{
		investor: isInvestor,
		Investor: isInvestor,
		Balance:  floatAt(b, 9),
		Credit:   floatAt(b, 17),
		Currency: readText(b[25:89]),
		Leverage: le.Uint32(b[93:97]),
		Server:   readText(b[355:483]),
		Margin:   floatAt(b, 772),
	}, nil
}

const positionRecordSize, orderRecordSize = 344, 356

func decodeOpenState(b []byte) ([]Position, []PendingOrder, error) {
	positions, orders, err := recordSections(b, positionRecordSize, orderRecordSize)
	if err != nil {
		return nil, nil, err
	}
	open := make([]Position, 0, len(positions)/positionRecordSize)
	for p := range records(positions, positionRecordSize) {
		open = append(open, Position{
			ID: le.Uint64(p), Symbol: readText(p[24:88]), Side: le.Uint32(p[88:92]),
			OpenTime: le.Uint32(p[16:20]), Digits: le.Uint32(p[260:264]), VolumeUnits: le.Uint64(p[124:132]),
			OpenPrice: floatAt(p, 92), CurrentPrice: floatAt(p, 100), SL: floatAt(p, 108), TP: floatAt(p, 116),
			Profit: floatAt(p, 132), Commission: floatAt(p, 156), Swap: floatAt(p, 164),
		})
	}
	pending := make([]PendingOrder, 0, len(orders)/orderRecordSize)
	for p := range records(orders, orderRecordSize) {
		orderType := le.Uint32(p[148:152])
		// Open state can include market requests and Close By. The pending
		// list consists of the six standing limit, stop and stop-limit types.
		if orderType < 2 || orderType > 7 {
			continue
		}
		pending = append(pending, PendingOrder{
			Ticket: le.Uint64(p), Symbol: readText(p[72:136]), Type: orderType, SetupTime: le.Uint32(p[136:140]),
			Digits: le.Uint32(p[312:316]), RemainingVolumeUnits: le.Uint64(p[212:220]),
			Price: floatAt(p, 164), TriggerPrice: floatAt(p, 172), CurrentPrice: floatAt(p, 180), SL: floatAt(p, 188), TP: floatAt(p, 196),
		})
	}
	return open, pending, nil
}

func recordSections(body []byte, firstSize, secondSize int) ([]byte, []byte, error) {
	if len(body) < 8 {
		return nil, nil, ErrProtocol
	}
	count := int(le.Uint32(body))
	if count > (len(body)-8)/firstSize {
		return nil, nil, ErrProtocol
	}
	offset := 4 + count*firstSize
	secondCount := int(le.Uint32(body[offset:]))
	rest := body[offset+4:]
	if len(rest)%secondSize != 0 || secondCount != len(rest)/secondSize {
		return nil, nil, ErrProtocol
	}
	return body[4:offset], rest, nil
}

func records(section []byte, size int) func(func([]byte) bool) {
	return func(yield func([]byte) bool) {
		for offset := 0; offset < len(section); offset += size {
			if !yield(section[offset : offset+size]) {
				return
			}
		}
	}
}

func floatAt(b []byte, offset int) float64 {
	return math.Float64frombits(le.Uint64(b[offset:]))
}

func readText(src []byte) string {
	units := make([]uint16, 0, len(src)/2)
	for i := 0; i < len(src); i += 2 {
		unit := le.Uint16(src[i:])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}
