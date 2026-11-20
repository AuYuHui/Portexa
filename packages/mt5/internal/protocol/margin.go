package protocol

import (
	"encoding/binary"
	"math"
)

const (
	fullCatalogHeaderSize = 7365 // Status, tag 0x07, group record, cache mode and count.
	symbolCatalogRowSize  = 3716
	symbolInfoSize        = 1952
	cfdLeverageMode       = 4
	hedgedUseLargerLeg    = 4
	nativePositionSize    = 672
)

type marginSymbol struct {
	contract, hedgedContract float64
	rates                    [2]float64
	largerLeg                bool
}

func marginSymbols(catalog []byte) map[string]marginSymbol {
	if len(catalog) < fullCatalogHeaderSize || binary.LittleEndian.Uint32(catalog[:4]) != 0 || catalog[4] != 0x07 ||
		binary.LittleEndian.Uint32(catalog[7357:7361]) != 0 {
		return nil
	}
	count := binary.LittleEndian.Uint32(catalog[7361:7365])
	catalog = catalog[fullCatalogHeaderSize:]
	if count == 0 || count > uint32(len(catalog)/symbolCatalogRowSize) {
		return nil
	}
	symbols := make(map[string]marginSymbol)
	seen := make(map[string]bool)
	for range count {
		row := catalog[:symbolCatalogRowSize]
		catalog = catalog[symbolCatalogRowSize:]
		name := decodeUTF16String(row[8:72])
		if name == "" || seen[name] {
			return nil
		}
		seen[name] = true
		if binary.LittleEndian.Uint32(row[1660:1664]) != cfdLeverageMode {
			continue
		}
		group := row[symbolInfoSize:]
		// This path supports position-based CFD leverage. Fixed initial and
		// maintenance amounts use a separate broker calculation rule.
		if floatAt(group, 836) != 0 || floatAt(group, 844) != 0 {
			continue
		}
		spec := marginSymbol{
			contract: floatAt(row, 1648), hedgedContract: floatAt(group, 988),
			largerLeg: binary.LittleEndian.Uint32(group[1080:1084])&hedgedUseLargerLeg != 0,
		}
		for side := range spec.rates {
			spec.rates[side] = floatAt(group, 916+8*side)
			if spec.rates[side] == 0 {
				spec.rates[side] = floatAt(group, 852+8*side)
			}
		}
		if positiveFinite(spec.contract) && nonnegativeFinite(spec.hedgedContract) &&
			positiveFinite(spec.rates[0]) && positiveFinite(spec.rates[1]) {
			symbols[name] = spec
		}
	}
	return symbols
}

type marginLeg struct {
	lots, openNotional, conversion float64
}

func calculateNativeMargin(catalog, trades []byte, result *SyncResult) (float64, bool) {
	// A complete no-pending-orders reply has tag 0x1f, then tag 0x24
	// and a counted list of positions. The legacy scanner alone cannot
	// establish completeness for an account-wide amount.
	const positionsStart = 18
	if len(trades) < positionsStart || trades[0] != 0x1f || trades[9] != 0x24 ||
		binary.LittleEndian.Uint32(trades[5:9]) != 0 || result.Account.Leverage == 0 || len(result.Orders) != 0 {
		return 0, false
	}
	count := binary.LittleEndian.Uint32(trades[14:18])
	trades = trades[positionsStart:]
	if count == 0 || count != uint32(len(result.Positions)) || count > uint32(len(trades)/nativePositionSize) {
		return 0, false
	}
	specs := marginSymbols(catalog)
	if len(specs) == 0 {
		return 0, false
	}
	type exposure struct {
		legs [2]marginLeg
	}
	bySymbol := make(map[string]*exposure)
	var currencyDigits uint32
	for i := range count {
		row := trades[:nativePositionSize]
		trades = trades[nativePositionSize:]
		if binary.LittleEndian.Uint64(row[72:80]) != result.Account.Login {
			return 0, false
		}
		name := decodeUTF16String(row[120:184])
		side := binary.LittleEndian.Uint32(row[184:188])
		openPrice, conversion := floatAt(row, 192), floatAt(row, 248)
		volume := binary.LittleEndian.Uint64(row[224:232])
		digits := binary.LittleEndian.Uint32(row[372:376])
		spec, found := specs[name]
		if !found || side > 1 || !positiveFinite(openPrice) || !positiveFinite(conversion) ||
			volume == 0 || floatAt(row, 360) != spec.contract || digits > 8 || (i > 0 && digits != currencyDigits) {
			return 0, false
		}
		currencyDigits = digits
		if bySymbol[name] == nil {
			bySymbol[name] = &exposure{}
		}
		leg := &bySymbol[name].legs[side]
		// The observed CFD configuration uses one account-currency rate per
		// side. Leave mixed conversions unavailable until their rounding and
		// weighting have been verified.
		if leg.lots != 0 && leg.conversion != conversion {
			return 0, false
		}
		lots := float64(volume) / 100_000_000
		leg.lots += lots
		leg.openNotional += lots * openPrice
		leg.conversion = conversion
	}
	var total float64
	for symbol, exposure := range bySymbol {
		total += cfdMargin(specs[symbol], exposure.legs, float64(result.Account.Leverage))
	}
	scale := math.Pow10(int(currencyDigits))
	rounded := math.Round(total*scale) / scale
	return rounded, positiveFinite(rounded)
}

func cfdMargin(spec marginSymbol, legs [2]marginLeg, leverage float64) float64 {
	buy, sell := legs[0], legs[1]
	buyMargin := buy.openNotional * spec.contract * buy.conversion * spec.rates[0] / leverage
	sellMargin := sell.openNotional * spec.contract * sell.conversion * spec.rates[1] / leverage
	if spec.largerLeg || buy.lots == 0 || sell.lots == 0 {
		return max(buyMargin, sellMargin)
	}
	covered := min(buy.lots, sell.lots)
	totalLots := buy.lots + sell.lots
	hedgedPrice := (buy.openNotional + sell.openNotional) / totalLots
	hedgedConversion := (buy.conversion*buy.lots + sell.conversion*sell.lots) / totalLots
	hedgedRate := (spec.rates[0] + spec.rates[1]) / 2
	hedged := covered * hedgedPrice * spec.hedgedContract * hedgedConversion * hedgedRate / leverage
	return hedged + buyMargin*(buy.lots-covered)/buy.lots + sellMargin*(sell.lots-covered)/sell.lots
}

func floatAt(b []byte, offset int) float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(b[offset : offset+8]))
}

func nonnegativeFinite(value float64) bool {
	return value >= 0 && !math.IsInf(value, 0)
}

func positiveFinite(value float64) bool {
	return value > 0 && !math.IsInf(value, 0)
}
