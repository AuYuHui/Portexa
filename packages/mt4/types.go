// Package mt4 implements the verified read-only MT4 mobile protocol profile.
// It has no CLI, storage, aggregation, or terminal runtime dependency.
package mt4

// PasswordType is a caller declaration; the server indicator is not proof of it.
type PasswordType string

const InvestorPassword PasswordType = "investor"

// Config selects an independently verified endpoint. Password stays in memory.
type Config struct {
	Address      string
	Server       string
	Login        uint32
	Password     string
	PasswordType PasswordType
}

// HistoryRange sends MT4 timestamp seconds unchanged. The caller handles the
// broker's wall-clock timezone; these values are not automatically UTC instants.
// End must be greater than Start and no greater than 2147483647.
type HistoryRange struct {
	Start, End uint32
}

// Options controls optional history synchronization. Nil History skips it.
type Options struct {
	History *HistoryRange
	// HistoryWindowSeconds splits a query into overlapping time windows. Zero
	// requests one block. Ticket-based reconciliation removes boundary duplicates.
	HistoryWindowSeconds uint32
}

// Account exposes decoded protocol values, not inferred trading permissions.
type Account struct {
	Leverage                         uint32
	Balance, Credit                  float64
	InvestorIndicator, DemoIndicator uint32
	Currency                         string
	// Equity is calculated as balance + credit + net open-position P/L, using
	// the synchronized order values, rather than a later quote or UI refresh.
	Equity float64
	Margin float64
	// False means margin could not be calculated from the synchronized profile.
	MarginAvailable bool
}

// Trade exposes the synchronized order fields without business aggregation.
// Command: 0 buy, 1 sell, 2..5 pending, 6 balance, 7 credit.
type Trade struct {
	Ticket int32
	Symbol string
	// Comment is the server's NUL-terminated 32-byte order annotation.
	// It is source data, not a deposit/withdrawal classification.
	Comment          string
	Digits, Command  int32
	VolumeHundredths int32
	Time20, Time40   uint32
	// OpenTime/CloseTime retain broker wall-clock seconds. No UTC conversion is
	// implied; Time20/Time40 are retained as wire-offset aliases.
	OpenTime, CloseTime uint32
	// ClosePrice is the current price for an open position, and the closing
	// price for a closed historical order.
	OpenPrice, ClosePrice           float64
	SL, TP                          float64
	Commission, Swap, Profit, Taxes float64
}

// Lots converts the protocol's hundredths-of-a-lot integer without rounding it.
func (t Trade) Lots() float64 { return float64(t.VolumeHundredths) / 100 }

// NetProfit includes separately reported charges, preserving their signs.
func (t Trade) NetProfit() float64 { return t.Profit + t.Commission + t.Swap + t.Taxes }

// Table describes synchronization. Status 1 has no new body, not an empty cache.
type Table struct {
	Status byte
	Count  int
}

type RecordInfo struct {
	Kind, Mode uint16
	Bytes      int
}

type ServerInfo struct {
	Name         string
	ConfigBytes  int
	AccessPoints int
}

// Result is a protocol snapshot. Partial results can accompany a later error;
// consumers must check AccountRead/HistoryRead before using those fields.
// It deliberately contains no password, stream key, installation ID or payload.
type Result struct {
	ServerBuild          uint16
	InitialBytes         int
	FirstResponseDecoded bool
	FeatureRecords       []RecordInfo
	Server               ServerInfo
	SessionConfirmed     bool
	AccountRead          bool
	Account              Account
	AccountBaseBytes     int
	Trades               []Trade
	TradeTrailingBytes   int
	Symbols, Groups      Table
	Optional             Table
	HistoryRead          bool
	History              []Trade
	HistoryStatus        byte
	// HistoryRead and HistoryComplete are true only after every requested window
	// supplies a complete response, including an explicit empty status. On error,
	// History/HistoryCovered may be partial;
	// callers must not replace a stored snapshot with them.
	HistoryComplete bool
	HistoryCovered  []HistoryRange
}
