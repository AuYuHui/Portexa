package accounts

import (
	"cmp"
	"slices"
	"strconv"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
)

type DailyPositionExit struct {
	Identifier, Ticket int64
	Symbol             string
	Side               int32
	OpenTimeWall       uint32
	CloseWallMS        int64
	Reversal           bool
}

type confirmedPositionExit struct {
	DailyPositionExit
	afterMS, beforeMS int64
}

func summarizePositionExcursions(tx *gorm.DB, job DailyJob, account storage.Account, currency string) (storage.DailyPositionExcursions, error) {
	var summary storage.DailyPositionExcursions
	if len(job.PositionExits) == 0 {
		return summary, nil
	}
	ids := make(map[int64]bool)
	for _, exit := range job.PositionExits {
		ids[exit.Identifier] = true
	}
	var snapshot storage.Snapshot
	var active []storage.Position
	if job.MT5Config != nil && account.CurrentSnapshotID != nil {
		if err := tx.First(&snapshot, "account_id=? AND id=?", account.ID, *account.CurrentSnapshotID).Error; err != nil {
			return summary, err
		}
		if err := tx.Where("account_id=? AND snapshot_id=? AND command IN (0,1)", account.ID, snapshot.ID).Find(&active).Error; err != nil {
			return summary, err
		}
		for _, position := range active {
			ids[position.Ticket] = true
		}
	}
	rows, err := positionExcursions(tx, account.ID, account.UnitRevision, currency, ids)
	if err != nil {
		return summary, err
	}
	byIdentifier := make(map[int64][]storage.PositionExcursion)
	for _, row := range rows {
		byIdentifier[row.Identifier] = append(byIdentifier[row.Identifier], row)
	}
	var exits []confirmedPositionExit
	if job.MT5Config == nil {
		for _, exit := range job.PositionExits {
			exits = append(exits, confirmedPositionExit{DailyPositionExit: exit})
		}
	} else {
		exits, err = confirmMT5PositionExits(tx, job, snapshot, active, byIdentifier)
		if err != nil {
			return summary, err
		}
	}
	summary.Closed = len(exits)
	for _, exit := range exits {
		rows := matchingExcursions(byIdentifier[exit.Identifier], exit)
		if len(rows) > 0 {
			summary.Sampled++
		}
		for _, row := range rows {
			includePositionExtremes(&summary, row)
		}
	}
	return summary, nil
}

func confirmMT5PositionExits(tx *gorm.DB, job DailyJob, snapshot storage.Snapshot, active []storage.Position, rows map[int64][]storage.PositionExcursion) ([]confirmedPositionExit, error) {
	var server storage.Server
	if err := tx.First(&server, "id=?", job.Run.ServerID).Error; err != nil {
		return nil, err
	}
	// A server date can fall anywhere in the supported UTC offset range.
	padding := (15 * time.Hour).Milliseconds()
	entries, _, err := historicalTimeEntries(tx.Statement.Context, tx, server, int64(job.Day.Start)*1000-padding, int64(job.Day.End)*1000+padding)
	if err != nil {
		return nil, err
	}
	events := make([]confirmedPositionExit, 0, len(job.PositionExits))
	unresolved := make(map[int64]bool)
	for _, exit := range job.PositionExits {
		at, ok := historyOrderUTC(storage.HistoryOrder{MT5WallTimeMS: &exit.CloseWallMS}, entries)
		if !ok {
			unresolved[exit.Identifier] = true
			continue
		}
		events = append(events, confirmedPositionExit{DailyPositionExit: exit, beforeMS: at})
	}
	slices.SortFunc(events, func(a, b confirmedPositionExit) int {
		if order := cmp.Compare(a.beforeMS, b.beforeMS); order != 0 {
			return order
		}
		return cmp.Compare(a.Ticket, b.Ticket)
	})
	reversedAt := make(map[int64]int64)
	pending := make(map[int64]confirmedPositionExit)
	exits := []confirmedPositionExit{}
	for _, exit := range events {
		if unresolved[exit.Identifier] {
			continue
		}
		exit.afterMS = reversedAt[exit.Identifier]
		if exit.Reversal {
			exits = append(exits, exit)
			reversedAt[exit.Identifier] = exit.beforeMS
			delete(pending, exit.Identifier)
		} else {
			pending[exit.Identifier] = exit
		}
	}
	open := make(map[int64]storage.Position)
	unknown := make(map[string]bool)
	for _, position := range active {
		if slices.ContainsFunc(rows[position.Ticket], func(row storage.PositionExcursion) bool {
			return row.LastSampleMS == snapshot.ObservedToMS && row.Symbol == position.Symbol && row.Command == position.Command
		}) {
			open[position.Ticket] = position
		} else {
			unknown[position.Symbol] = true
		}
	}
	for _, exit := range pending {
		// A plain exit can be a partial fill. Confirm that a successful
		// later snapshot has no remaining position in the closing side.
		if snapshot.ObservedFromMS <= exit.beforeMS || unknown[exit.Symbol] {
			continue
		}
		if position, ok := open[exit.Identifier]; ok && position.Command == exit.Side {
			continue
		}
		exits = append(exits, exit)
	}
	return exits, nil
}

func matchingExcursions(rows []storage.PositionExcursion, exit confirmedPositionExit) []storage.PositionExcursion {
	if exit.beforeMS == 0 {
		return slices.DeleteFunc(slices.Clone(rows), func(row storage.PositionExcursion) bool {
			return row.Command != exit.Side || row.Symbol != exit.Symbol || row.OpenTimeWall != exit.OpenTimeWall
		})
	}
	var latest int64
	var openTime uint32
	for _, row := range rows {
		if row.LastSampleMS > latest && row.FirstSampleMS >= exit.afterMS && row.LastSampleMS < exit.beforeMS {
			latest, openTime = row.LastSampleMS, row.OpenTimeWall
			if row.Command != exit.Side || row.Symbol != exit.Symbol {
				openTime = 0
			}
		}
	}
	return slices.DeleteFunc(slices.Clone(rows), func(row storage.PositionExcursion) bool {
		return openTime == 0 || row.OpenTimeWall != openTime || row.Command != exit.Side || row.Symbol != exit.Symbol ||
			row.FirstSampleMS < exit.afterMS || row.LastSampleMS >= exit.beforeMS
	})
}

func includePositionExtremes(summary *storage.DailyPositionExcursions, row storage.PositionExcursion) {
	ticket := strconv.FormatInt(row.Ticket, 10)
	loss := summary.Loss
	if row.Lowest.IsNegative() && (loss == nil || row.Lowest.LessThan(loss.Amount) ||
		row.Lowest.Equal(loss.Amount) && earlierPoint(row.LowestMS, ticket, loss)) {
		summary.Loss = &storage.PositionExcursionPoint{Amount: row.Lowest, Ticket: ticket, Symbol: row.Symbol, AtMS: row.LowestMS}
	}
	profit := summary.Profit
	if row.Highest.IsPositive() && (profit == nil || row.Highest.GreaterThan(profit.Amount) ||
		row.Highest.Equal(profit.Amount) && earlierPoint(row.HighestMS, ticket, profit)) {
		summary.Profit = &storage.PositionExcursionPoint{Amount: row.Highest, Ticket: ticket, Symbol: row.Symbol, AtMS: row.HighestMS}
	}
}

func earlierPoint(at int64, ticket string, point *storage.PositionExcursionPoint) bool {
	return at < point.AtMS || at == point.AtMS && ticket < point.Ticket
}
