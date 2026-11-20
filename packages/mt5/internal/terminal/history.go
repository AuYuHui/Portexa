package terminal

type HistoryRange struct {
	Start, End uint32 // Half-open broker wall-clock seconds.
}

type HistoryResult struct {
	Account Account
	Deals   []Deal
}

type DealType uint32

const (
	DealBuy DealType = iota
	DealSell
	DealBalance
	DealCredit
	DealCharge
	DealCorrection
	DealBonus
	DealCommission
	DealCommissionDaily
	DealCommissionMonthly
	DealCommissionAgentDaily
	DealCommissionAgentMonthly
	DealInterest
	DealBuyCanceled
	DealSellCanceled
	DealDividend
	DealDividendFranked
	DealTax
)

type DealEntry uint32

const (
	EntryIn DealEntry = iota
	EntryOut
	EntryInOut
	EntryOutBy
)

type Deal struct {
	Ticket, Order, PositionID     uint64
	WallTimeMS                    int64
	Symbol, Comment               string
	Type                          DealType
	Entry                         DealEntry
	Digits                        uint32
	VolumeUnits                   uint64
	Price, SL, TP                 float64
	Profit, Commission, Swap, Fee float64
}

func (s session) readHistory(window HistoryRange) ([]Deal, error) {
	// The Web Terminal uses signed Unix seconds. Some access servers apply
	// an offset to the query bounds, while records carry broker wall time.
	// Overfetch by one day and enforce the exact half-open window locally.
	const day = int64(24 * 3600)
	from := max(0, int64(window.Start)-day)
	to := min(int64(1<<31-1), int64(window.End)+day)
	payload := le.AppendUint32(nil, uint32(from))
	payload = le.AppendUint32(payload, uint32(to))
	body, err := s.request(cmdHistory, payload)
	if err != nil {
		return nil, err
	}
	return decodeDeals(body, window)
}

func decodeDeals(body []byte, window HistoryRange) ([]Deal, error) {
	const dealSize = 356
	deals, _, err := recordSections(body, dealSize, orderRecordSize)
	if err != nil {
		return nil, err
	}
	out := make([]Deal, 0, len(deals)/dealSize)
	for p := range records(deals, dealSize) {
		seconds := le.Uint32(p[80:84])
		if seconds == 0 {
			return nil, ErrProtocol
		}
		if seconds < window.Start || seconds >= window.End {
			continue
		}
		milliseconds := le.Uint32(p[340:344])
		if milliseconds >= 1000 {
			return nil, ErrProtocol
		}
		out = append(out, Deal{
			Ticket: le.Uint64(p), Order: le.Uint64(p[72:80]), PositionID: le.Uint64(p[248:256]),
			WallTimeMS: int64(seconds)*1000 + int64(milliseconds), Symbol: readText(p[88:152]), Comment: readText(p[256:320]),
			Type: DealType(le.Uint32(p[152:156])), Entry: DealEntry(le.Uint32(p[156:160])), Digits: le.Uint32(p[328:332]),
			VolumeUnits: le.Uint64(p[192:200]), Price: floatAt(p, 160), SL: floatAt(p, 176), TP: floatAt(p, 184),
			Profit: floatAt(p, 200), Commission: floatAt(p, 224), Swap: floatAt(p, 232), Fee: floatAt(p, 348),
		})
	}
	return out, nil
}
