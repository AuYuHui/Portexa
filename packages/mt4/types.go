// Package mt4 implements the verified read-only MT4 mobile protocol profile.
// It has no CLI, storage, aggregation, or terminal runtime dependency.
package mt4

// Config selects an independently verified endpoint. Password stays in memory.
type Config struct {
	Address  string
	Server   string
	Login    uint32
	Password string
}

// HistoryRange uses Unix seconds, with Start inclusive and End greater than Start.
// Server retention and interval semantics beyond the verified profile may vary.
type HistoryRange struct {
	Start, End uint32
}

// Options controls optional history synchronization. Nil History skips it.
type Options struct {
	History *HistoryRange
}

// Account exposes decoded protocol values, not inferred trading permissions.
type Account struct {
	Leverage                         uint32
	Balance, Credit                  float64
	InvestorIndicator, DemoIndicator uint32
}

// Trade retains wire time offsets until their UI mapping is fully verified.
// Command: 0 buy, 1 sell, 2..5 pending, 6 balance, 7 credit.
type Trade struct {
	Ticket                          int32
	Symbol                          string
	Digits, Command                 int32
	VolumeHundredths                int32
	Time20, Time40                  uint32
	SL, TP                          float64
	Commission, Swap, Profit, Taxes float64
}

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
}
