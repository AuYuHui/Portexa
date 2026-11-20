package mt5

import "portexa/mt5/internal/terminal"

type DealType = terminal.DealType
type DealEntry = terminal.DealEntry
type Deal = terminal.Deal

const (
	DealBuy                    = terminal.DealBuy
	DealSell                   = terminal.DealSell
	DealBalance                = terminal.DealBalance
	DealCredit                 = terminal.DealCredit
	DealCharge                 = terminal.DealCharge
	DealCorrection             = terminal.DealCorrection
	DealBonus                  = terminal.DealBonus
	DealCommission             = terminal.DealCommission
	DealCommissionDaily        = terminal.DealCommissionDaily
	DealCommissionMonthly      = terminal.DealCommissionMonthly
	DealCommissionAgentDaily   = terminal.DealCommissionAgentDaily
	DealCommissionAgentMonthly = terminal.DealCommissionAgentMonthly
	DealInterest               = terminal.DealInterest
	DealBuyCanceled            = terminal.DealBuyCanceled
	DealSellCanceled           = terminal.DealSellCanceled
	DealDividend               = terminal.DealDividend
	DealDividendFranked        = terminal.DealDividendFranked
	DealTax                    = terminal.DealTax

	EntryIn    = terminal.EntryIn
	EntryOut   = terminal.EntryOut
	EntryInOut = terminal.EntryInOut
	EntryOutBy = terminal.EntryOutBy
)
