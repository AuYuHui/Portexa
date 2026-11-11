package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
)

type marginSymbol struct {
	mode                                  uint32
	contract, tickValue, tickSize         float64
	initial, maintenance, hedged, divider float64
}

type marginSide struct{ volume, price, rate float64 }
type marginPosition struct{ buy, sell marginSide }

func moneyAt(data []byte, offset int) float64 {
	return math.Float64frombits(binary.LittleEndian.Uint64(data[offset:]))
}

// CalculateMargin follows the analyzed client's group overrides and per-symbol
// margin modes. Prices and conversion rates come from the same account block;
// it does not mix in a later quote or infer margin from balance/equity.
func CalculateMargin(account *AccountInfo, symbols []byte) error {
	positions := map[string]*marginPosition{}
	for _, order := range account.Orders {
		if order.Command != 0 && order.Command != 1 {
			continue
		}
		if order.Symbol == "" || order.VolumeHundredths <= 0 || order.MarginRate <= 0 {
			return errors.New("position has no usable margin conversion rate or volume")
		}
		p := positions[order.Symbol]
		if p == nil {
			p = &marginPosition{}
			positions[order.Symbol] = p
		}
		side := &p.buy
		if order.Command == 1 {
			side = &p.sell
		}
		volume := float64(order.VolumeHundredths)
		side.volume += volume
		side.price += volume * order.OpenPrice
		side.rate += volume * order.MarginRate
	}
	if len(positions) == 0 {
		account.MarginAvailable = true
		return nil
	}
	if len(account.Raw) < AccountBaseSize || len(symbols)%1936 != 0 {
		return errors.New("margin configuration has an invalid boundary")
	}
	configs := map[string][]byte{}
	for len(symbols) > 0 {
		name := symbols[:12]
		if end := bytes.IndexByte(name, 0); end >= 0 {
			name = name[:end]
		}
		configs[string(name)] = symbols[:1936]
		symbols = symbols[1936:]
	}
	// The symbol manager stores the copied group at +0x10; its overrides at
	// +0x10a8 and count at +0x34a8 map to these account wire offsets.
	overrides := binary.LittleEndian.Uint32(account.Raw[0x34f0:])
	if overrides > 128 {
		return errors.New("margin override count exceeds group layout")
	}
	largestSide := binary.LittleEndian.Uint32(account.Raw[0x3540:]) != 0
	names := make([]string, 0, len(positions))
	for name := range positions {
		names = append(names, name)
	}
	sort.Strings(names)
	total := 0.0
	for _, name := range names {
		data, found := configs[name]
		if !found {
			return fmt.Errorf("%w: missing position symbol configuration", ErrUnsupportedProtocol)
		}
		config := marginSymbol{
			mode:     binary.LittleEndian.Uint32(data[0x6c8:]),
			contract: moneyAt(data, 0x6a8), tickValue: moneyAt(data, 0x6b0), tickSize: moneyAt(data, 0x6b8),
			initial: moneyAt(data, 0x6d0), maintenance: moneyAt(data, 0x6d8), hedged: moneyAt(data, 0x6e0), divider: moneyAt(data, 0x6e8),
		}
		for index := uint32(0); index < overrides; index++ {
			row := account.Raw[0x10f0+int(index)*72:][:72]
			label := row[:12]
			if end := bytes.IndexByte(label, 0); end >= 0 {
				label = label[:end]
			}
			if string(label) == name {
				if divider := moneyAt(row, 0x20); divider != math.MaxFloat64 {
					config.divider = divider
				}
				break
			}
		}
		value, err := symbolMargin(config, *positions[name], float64(account.Leverage), largestSide)
		if err != nil {
			return err
		}
		total += value
	}
	account.Margin = total / 100
	if math.IsNaN(account.Margin) || math.IsInf(account.Margin, 0) || account.Margin < 0 {
		return errors.New("calculated margin is not finite or nonnegative")
	}
	account.MarginAvailable = true
	return nil
}

func symbolMargin(config marginSymbol, position marginPosition, leverage float64, largestSide bool) (float64, error) {
	if config.mode > 4 {
		return 0, fmt.Errorf("%w: margin mode", ErrUnsupportedProtocol)
	}
	for _, value := range []float64{config.contract, config.tickValue, config.tickSize, config.initial, config.maintenance, config.hedged, config.divider} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, errors.New("invalid margin configuration value")
		}
	}
	if config.divider <= 0 || ((config.mode == 0 || config.mode == 4) && leverage <= 0) || (config.mode == 3 && (config.tickSize <= 0 || config.tickValue <= 0)) {
		return 0, errors.New("invalid margin divisor")
	}
	unit := func(price float64, hedged bool) float64 {
		base := config.initial
		if config.mode == 2 {
			base = config.maintenance
		} else if base <= 0 {
			base = config.contract
			if config.mode != 0 {
				base *= price
			}
			if config.mode == 3 {
				base = base / config.tickSize * config.tickValue
			}
		}
		if hedged {
			base = config.hedged
			if config.initial <= 0 && (config.mode == 1 || config.mode == 3 || config.mode == 4) {
				base *= price
			}
			if config.initial <= 0 && config.mode == 3 {
				base = base / config.tickSize * config.tickValue
			}
		}
		if config.mode == 0 || config.mode == 4 {
			base /= leverage
		}
		return base / config.divider
	}
	sideMargin := func(side marginSide) float64 {
		if side.volume == 0 {
			return 0
		}
		return side.volume * unit(side.price/side.volume, false) * (side.rate / side.volume)
	}
	if largestSide && position.buy.volume > 0 && position.sell.volume > 0 {
		return math.Max(sideMargin(position.buy), sideMargin(position.sell)), nil
	}
	volume := position.buy.volume + position.sell.volume
	price := (position.buy.price + position.sell.price) / volume
	rate := (position.buy.rate + position.sell.rate) / volume
	unhedged := math.Abs(position.buy.volume - position.sell.volume)
	return (unhedged*unit(price, false) + (volume-unhedged)*unit(price, true)) * rate, nil
}
