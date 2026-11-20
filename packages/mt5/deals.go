package mt5

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
