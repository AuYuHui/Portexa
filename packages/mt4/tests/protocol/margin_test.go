package protocol_test

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"testing"

	. "portexa/mt4/internal/protocol"
)

func syntheticMarginAccount(buy, sell int32) AccountInfo {
	account := AccountInfo{Leverage: 100, Raw: make([]byte, AccountBaseSize)}
	if buy > 0 {
		account.Orders = append(account.Orders, TradeInfo{Ticket: 1, Symbol: "TEST.USD", VolumeHundredths: buy, OpenPrice: 1.125, MarginRate: 1.25})
	}
	if sell > 0 {
		account.Orders = append(account.Orders, TradeInfo{Ticket: 2, Symbol: "TEST.USD", Command: 1, VolumeHundredths: sell, OpenPrice: 1.375, MarginRate: 1.75})
	}
	return account
}

func syntheticMarginSymbol(mode uint32, initial float64) []byte {
	symbol := make([]byte, 1936)
	copy(symbol, "TEST.USD")
	binary.LittleEndian.PutUint32(symbol[0x6c8:], mode)
	for offset, value := range map[int]float64{0x6a8: 100000, 0x6b0: 0.25, 0x6b8: 0.01, 0x6d0: initial, 0x6d8: 65, 0x6e0: 30, 0x6e8: 2} {
		binary.LittleEndian.PutUint64(symbol[offset:], math.Float64bits(value))
	}
	return symbol
}

func TestMarginMatchesIndependentNativeSyntheticVectors(t *testing.T) {
	// Expected results come from bounded offline execution of the installed
	// client's arithmetic routine on synthetic account/symbol configurations.
	// No terminal binary or emulator is needed by this test or the SDK.
	data, err := os.ReadFile("testdata/margin.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Mode        uint32  `json:"mode"`
		Initial     float64 `json:"initial"`
		LargestSide bool    `json:"largest_side"`
		Buy         int32   `json:"buy_volume"`
		Sell        int32   `json:"sell_volume"`
		Expected    float64 `json:"expected"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 80 {
		t.Fatal("incomplete independent margin vectors")
	}
	for index, vector := range vectors {
		account := syntheticMarginAccount(vector.Buy, vector.Sell)
		if vector.LargestSide {
			binary.LittleEndian.PutUint32(account.Raw[0x3540:], 1)
		}
		err := CalculateMargin(&account, syntheticMarginSymbol(vector.Mode, vector.Initial))
		if err != nil || !account.MarginAvailable || math.Abs(account.Margin-vector.Expected) > 1e-9*math.Max(1, math.Abs(vector.Expected)) {
			t.Fatalf("native synthetic margin vector %d: got %g, want %g, error %v", index, account.Margin, vector.Expected, err)
		}
	}
}

func TestMarginOverridesPendingOrdersAndMissingConfigurations(t *testing.T) {
	account := syntheticMarginAccount(125, 0)
	account.Orders = append(account.Orders, TradeInfo{Command: 2, Symbol: "PENDING", VolumeHundredths: 10000})
	binary.LittleEndian.PutUint32(account.Raw[0x34f0:], 1)
	copy(account.Raw[0x10f0:], "TEST.USD")
	binary.LittleEndian.PutUint64(account.Raw[0x1110:], math.Float64bits(4))
	if err := CalculateMargin(&account, syntheticMarginSymbol(0, 0)); err != nil || account.Margin != 390.625 {
		t.Fatalf("group override or pending-order exclusion failed: %v", err)
	}
	account = syntheticMarginAccount(125, 0)
	if err := CalculateMargin(&account, nil); err == nil || account.MarginAvailable {
		t.Fatal("missing symbol configuration was treated as zero margin")
	}
	account = syntheticMarginAccount(125, 0)
	account.Orders[0].MarginRate = 0
	if err := CalculateMargin(&account, syntheticMarginSymbol(0, 0)); err == nil {
		t.Fatal("missing conversion rate was accepted")
	}
	account = syntheticMarginAccount(125, 0)
	if err := CalculateMargin(&account, syntheticMarginSymbol(99, 0)); err == nil {
		t.Fatal("unknown margin mode was accepted")
	}
}
