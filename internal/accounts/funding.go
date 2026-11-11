package accounts

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/storage"
)

type FundingView struct {
	State              string              `json:"state"`
	NativeCurrency     string              `json:"native_currency"`
	Deposits           *decimal.Decimal    `json:"deposits"`
	Withdrawals        *decimal.Decimal    `json:"withdrawals"`
	UnclassifiedCount  int                 `json:"unclassified_count"`
	ConfirmedCount     int                 `json:"confirmed_count"`
	AutomaticCount     int                 `json:"automatic_count"`
	ManualCount        int                 `json:"manual_count"`
	CoverageComplete   bool                `json:"coverage_complete"`
	CoveredThroughWall uint32              `json:"covered_through_wall"`
	PendingWindows     int                 `json:"pending_windows"`
	BlockedWindows     int                 `json:"blocked_windows"`
	LastSyncAtMS       *int64              `json:"last_sync_at_ms"`
	Stale              bool                `json:"stale"`
	LastRun            *storage.HistoryRun `json:"last_run"`
}

func fundingHistory(tx *gorm.DB, account storage.Account, snapshot storage.Snapshot) *gorm.DB {
	return currentHistory(tx, account.ID).Where("o.unit_revision=? AND o.native_currency=?", account.UnitRevision, snapshot.NativeCurrency)
}

func withClassification(query *gorm.DB) *gorm.DB {
	return query.Joins(`LEFT JOIN cash_flow_classifications AS c ON c.account_id=o.account_id AND c.ticket=o.ticket
		AND c.revision=(SELECT MAX(c2.revision) FROM cash_flow_classifications AS c2 WHERE c2.account_id=o.account_id AND c2.ticket=o.ticket)`)
}

func readFundingView(tx *gorm.DB, account storage.Account, server storage.Server, snapshot *storage.Snapshot, now time.Time) (FundingView, error) {
	view := FundingView{State: "idle"}
	if snapshot == nil || snapshot.UnitRevision != account.UnitRevision {
		return view, nil
	}
	view.NativeCurrency = snapshot.NativeCurrency
	day, err := serverDay(tx, server, now)
	if err != nil && !errors.Is(err, ErrServerTime) {
		return view, err
	}
	var coverage []storage.HistoryCoverage
	if err == nil {
		if err := historyScope(tx, account, day).Order("start_wall,end_wall").Find(&coverage).Error; err != nil {
			return view, err
		}
	}
	known := false
	recentThrough := day.Start
	for _, window := range coverage {
		if window.CheckedAtMS != nil {
			known = true
			if view.LastSyncAtMS == nil || *window.CheckedAtMS > *view.LastSyncAtMS {
				value := *window.CheckedAtMS
				view.LastSyncAtMS = &value
			}
			if window.StartWall <= view.CoveredThroughWall && window.EndWall > view.CoveredThroughWall {
				view.CoveredThroughWall = window.EndWall
			}
			if window.StartWall <= recentThrough && window.EndWall > recentThrough && now.UnixMilli()-*window.CheckedAtMS <= (10*time.Minute).Milliseconds() {
				recentThrough = window.EndWall
			}
		}
		if window.State == "blocked" {
			view.BlockedWindows++
		}
		if window.CheckedAtMS == nil && window.State != "split" {
			view.PendingWindows++
		}
	}
	view.CoverageComplete = len(coverage) > 0 && view.CoveredThroughWall >= day.End
	view.Stale = known && recentThrough < day.End
	query := withClassification(fundingHistory(tx, account, *snapshot)).Where("o.command=6").
		Select(`o.net_profit,o.revision,o.comment,o.commission,o.swap,o.taxes,
			o.automatic_category,o.automatic_reason,o.automatic_rule_version,c.category,c.based_trade_revision`)
	rows, readErr := query.Rows()
	if readErr != nil {
		return view, readErr
	}
	deposit, withdrawal := decimal.Zero, decimal.Zero
	for rows.Next() {
		var row struct {
			storage.HistoryOrder
			Category           *string
			BasedTradeRevision *int64
		}
		if readErr = tx.ScanRows(rows, &row); readErr != nil {
			break
		}
		known = true
		category := ""
		manual := row.Category != nil && row.BasedTradeRevision != nil && *row.BasedTradeRevision == row.Revision
		if manual {
			category = *row.Category
		} else {
			category, _, _ = currentAutomaticFundResult(row.HistoryOrder)
		}
		if category == "" || category == "unclassified" {
			view.UnclassifiedCount++
			continue
		}
		view.ConfirmedCount++
		if manual {
			view.ManualCount++
		} else {
			view.AutomaticCount++
		}
		switch category {
		case "deposit":
			deposit = deposit.Add(row.NetProfit)
		case "withdrawal":
			withdrawal = withdrawal.Add(row.NetProfit.Abs())
		}
	}
	readErr = errors.Join(readErr, rows.Err(), rows.Close())
	if readErr != nil {
		return view, readErr
	}
	if known {
		view.Deposits, view.Withdrawals = &deposit, &withdrawal
		view.State = "partial"
	}
	if view.CoverageComplete {
		view.State = "ready"
	}
	var run storage.HistoryRun
	found := tx.Where("account_id=? AND unit_revision=? AND native_currency=?", account.ID, account.UnitRevision, snapshot.NativeCurrency).
		Order("started_at_ms DESC,id DESC").Limit(1).Find(&run)
	if found.Error != nil {
		return view, found.Error
	}
	if found.RowsAffected > 0 {
		view.LastRun = &run
		if run.State == "running" {
			view.State = "running"
		}
		if run.State == "failed" {
			view.State = "failed"
		}
	}
	if view.BlockedWindows > 0 {
		view.State = "blocked"
	}
	if err != nil {
		view.State = "time_zone_unconfirmed"
	}
	if !account.SyncEnabled || account.HistorySyncPaused {
		view.State = "paused"
	}
	return view, nil
}

type FundEntry struct {
	storage.HistoryOrder
	Classification *storage.CashFlowClassification `json:"classification"`
	NeedsReview    bool                            `json:"needs_review"`
}

type FundsPage struct {
	Items      []FundEntry `json:"items"`
	NextCursor string      `json:"next_cursor"`
}

func (s *Service) Funds(ctx context.Context, id, cursor string, limit int, unclassified bool) (FundsPage, error) {
	page := FundsPage{Items: make([]FundEntry, 0)}
	if limit < 1 || limit > 100 {
		return page, &InputError{"查询条数需要在1至100之间"}
	}
	var wall uint64
	var ticket int64
	if cursor != "" {
		parts := strings.Split(cursor, ":")
		if len(parts) != 2 {
			return page, &InputError{"分页游标无效"}
		}
		var err error
		wall, err = strconv.ParseUint(parts[0], 10, 32)
		if err != nil {
			return page, &InputError{"分页游标无效"}
		}
		ticket, err = strconv.ParseInt(parts[1], 10, 32)
		if err != nil || ticket <= 0 {
			return page, &InputError{"分页游标无效"}
		}
	}
	err := s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id=? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		if account.CurrentSnapshotID == nil {
			return nil
		}
		var snapshot storage.Snapshot
		if err := tx.First(&snapshot, "id=? AND account_id=?", *account.CurrentSnapshotID, id).Error; err != nil {
			return err
		}
		if snapshot.UnitRevision != account.UnitRevision {
			return nil
		}
		hasCursor := cursor != ""
		batchSize := limit + 1
		if unclassified {
			batchSize = max(batchSize, 50)
		}
		for {
			query := fundingHistory(tx, account, snapshot).Where("o.command IN (6,7)")
			if hasCursor {
				query = query.Where("o.event_time_wall<? OR (o.event_time_wall=? AND o.ticket<?)", wall, wall, ticket)
			}
			if unclassified {
				// Old cached results are candidates until the current rule evaluates
				// them. Valid manual corrections always retain precedence.
				query = withClassification(query).Where(`o.command=6 AND (CASE WHEN c.based_trade_revision=o.revision
					THEN c.category WHEN o.automatic_rule_version=? THEN o.automatic_category ELSE '' END)
					IN ('','unclassified')`, FundingRuleVersion)
			}
			var orders []storage.HistoryOrder
			if err := query.Select("o.*").Order("o.event_time_wall DESC,o.ticket DESC").Limit(batchSize).Scan(&orders).Error; err != nil {
				return err
			}
			for _, order := range orders {
				entry, err := readFundEntry(tx, order)
				if err != nil {
					return err
				}
				if unclassified && entry.Classification != nil && !entry.NeedsReview && entry.Classification.Category != "unclassified" {
					continue
				}
				if len(page.Items) == limit {
					last := page.Items[limit-1]
					page.NextCursor = fmt.Sprintf("%d:%d", last.EventTimeWall, last.Ticket)
					return nil
				}
				page.Items = append(page.Items, entry)
			}
			if !unclassified || len(orders) < batchSize {
				return nil
			}
			last := orders[len(orders)-1]
			wall, ticket, hasCursor = uint64(last.EventTimeWall), int64(last.Ticket), true
		}
	})
	return page, err
}

func readFundEntry(tx *gorm.DB, order storage.HistoryOrder) (FundEntry, error) {
	entry := FundEntry{HistoryOrder: order}
	var classification storage.CashFlowClassification
	found := tx.Where("account_id=? AND ticket=?", order.AccountID, order.Ticket).Order("revision DESC").Limit(1).Find(&classification)
	if found.Error != nil {
		return entry, found.Error
	}
	var manual *storage.CashFlowClassification
	if found.RowsAffected > 0 {
		manual = &classification
	}
	entry.Classification, entry.NeedsReview = effectiveFundClassification(order, manual)
	return entry, nil
}
