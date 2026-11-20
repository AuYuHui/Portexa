package mt5

type Account struct {
	Server          string
	Currency        string
	Balance         float64
	Credit          float64
	Leverage        uint32
	Investor        bool
	Margin          float64
	MarginAvailable bool
}

func (a Account) IsInvestor() bool {
	return a.Investor
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
}
