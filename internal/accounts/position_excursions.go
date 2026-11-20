package accounts

import (
	"maps"
	"slices"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"portexa/internal/storage"
)

type excursionKey struct {
	identifier, ticket int64
	command            int32
	openTime           uint32
}

func keyForExcursion(row storage.PositionExcursion) excursionKey {
	return excursionKey{row.Identifier, row.Ticket, row.Command, row.OpenTimeWall}
}

func recordPositionExcursions(tx *gorm.DB, snapshot storage.Snapshot, positions []storage.Position, isMT5 bool) error {
	observed := make(map[excursionKey]storage.PositionExcursion)
	ids := make(map[int64]bool)
	stamp := snapshot.ObservedToMS
	for _, position := range positions {
		if position.Command < 0 || position.Command > 1 {
			continue
		}
		identifier, ticket := position.Ticket, position.Ticket
		if isMT5 {
			const maxSQLiteID = 1<<63 - 1
			if position.MT5Identifier > maxSQLiteID || position.MT5NativeTicket > maxSQLiteID {
				continue
			}
			identifier, ticket = int64(position.MT5Identifier), int64(position.MT5NativeTicket)
		}
		if identifier <= 0 || ticket <= 0 || position.OpenTimeWall == 0 {
			continue
		}
		net := position.NetProfit()
		row := storage.PositionExcursion{
			AccountID: snapshot.AccountID, UnitRevision: snapshot.UnitRevision, NativeCurrency: snapshot.NativeCurrency,
			Identifier: identifier, Ticket: ticket, Command: position.Command, OpenTimeWall: position.OpenTimeWall, Symbol: position.Symbol,
			FirstSampleMS: stamp, LastSampleMS: stamp, Lowest: net, LowestMS: stamp, Highest: net, HighestMS: stamp,
		}
		observed[keyForExcursion(row)] = row
		ids[identifier] = true
	}
	if len(observed) == 0 {
		return nil
	}
	existing, err := positionExcursions(tx, snapshot.AccountID, snapshot.UnitRevision, snapshot.NativeCurrency, ids)
	if err != nil {
		return err
	}
	for _, row := range existing {
		key := keyForExcursion(row)
		sample, found := observed[key]
		if !found || row.Symbol != sample.Symbol || stamp <= row.LastSampleMS {
			if found {
				delete(observed, key)
			}
			continue
		}
		row.LastSampleMS = stamp
		if sample.Lowest.LessThan(row.Lowest) {
			row.Lowest, row.LowestMS = sample.Lowest, stamp
		}
		if sample.Highest.GreaterThan(row.Highest) {
			row.Highest, row.HighestMS = sample.Highest, stamp
		}
		observed[key] = row
	}
	if len(observed) == 0 {
		return nil
	}
	rows := slices.Collect(maps.Values(observed))
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(&rows, 60).Error
}

func positionExcursions(tx *gorm.DB, accountID string, revision int64, currency string, ids map[int64]bool) ([]storage.PositionExcursion, error) {
	var rows []storage.PositionExcursion
	for group := range slices.Chunk(slices.Collect(maps.Keys(ids)), 500) {
		var batch []storage.PositionExcursion
		if err := tx.Where("account_id=? AND unit_revision=? AND native_currency=? AND identifier IN ?", accountID, revision, currency, group).
			Find(&batch).Error; err != nil {
			return nil, err
		}
		rows = append(rows, batch...)
	}
	return rows, nil
}
