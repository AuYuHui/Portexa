package accounts

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"github.com/shopspring/decimal"
	"portexa/internal/storage"
)

const trendColumns = "id,observed_from_ms,observed_to_ms,native_currency,usd_scale,unit_revision,balance,credit,equity,floating_net_profit,collection_interval_seconds"

// Read immutable samples in bounded pages, releasing the SQLite cursor before
// reduction or HTTP output. The (time,id) cursor also preserves equal-time rows.
func (s *Service) visitTrendSnapshots(ctx context.Context, id string, from, to int64, visit func(storage.Snapshot) error) error {
	const batchSize = 2048
	stamp, cursor := from, ""
	for {
		query := s.Store.Read.WithContext(ctx).Model(&storage.Snapshot{}).Select(trendColumns).
			Where("account_id=? AND observed_to_ms>=? AND observed_to_ms<?", id, from, to)
		if cursor != "" {
			query = query.Where("(observed_to_ms,id)>(?,?)", stamp, cursor)
		}
		rows, err := query.Order("observed_to_ms,id").Limit(batchSize).Rows()
		if err != nil {
			return err
		}
		batch, err := readTrendBatch(rows, id)
		if err != nil {
			return err
		}
		for _, snapshot := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(snapshot); err != nil {
				return err
			}
		}
		if len(batch) < batchSize {
			return nil
		}
		last := batch[len(batch)-1]
		stamp, cursor = last.ObservedToMS, last.ID
	}
}

func readTrendBatch(rows *sql.Rows, accountID string) ([]storage.Snapshot, error) {
	batch := make([]storage.Snapshot, 0, 2048)
	for rows.Next() {
		var sample storage.Snapshot
		var scale sql.NullString
		var cadence sql.NullInt64
		sample.AccountID = accountID
		if err := rows.Scan(&sample.ID, &sample.ObservedFromMS, &sample.ObservedToMS, &sample.NativeCurrency, &scale,
			&sample.UnitRevision, &sample.Balance, &sample.Credit, &sample.Equity, &sample.FloatingNetProfit, &cadence); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		if scale.Valid {
			value, err := decimal.NewFromString(scale.String)
			if err != nil {
				return nil, errors.Join(err, rows.Close())
			}
			sample.USDScale = &value
		}
		if cadence.Valid {
			value := int(cadence.Int64)
			sample.CollectionIntervalSeconds = &value
		}
		batch = append(batch, sample)
	}
	return batch, errors.Join(rows.Err(), rows.Close())
}

func (s *Service) visitPortfolioSnapshots(ctx context.Context, id string, from, to int64, visit func(storage.Snapshot) error) error {
	afterBucket := (from+59999)/60000 - 1
	for {
		// Only integer timestamps are grouped. Money comes from the selected
		// original row, with the same deterministic timestamp/id tie-breaker.
		query := "SELECT s." + strings.ReplaceAll(trendColumns, ",", ",s.") + `
			FROM (
				SELECT (observed_to_ms+59999)/60000 AS bucket, MAX(observed_to_ms) AS stamp
				FROM account_snapshots
				WHERE account_id=? AND observed_to_ms>=? AND observed_to_ms<?
				  AND (observed_to_ms+59999)/60000>?
				GROUP BY (observed_to_ms+59999)/60000
				ORDER BY (observed_to_ms+59999)/60000 LIMIT 2048
			) AS b JOIN account_snapshots AS s ON s.account_id=? AND s.observed_to_ms=b.stamp
			WHERE s.id=(SELECT id FROM account_snapshots WHERE account_id=s.account_id
				AND observed_to_ms=s.observed_to_ms ORDER BY id DESC LIMIT 1)
			ORDER BY b.bucket`
		rows, err := s.Store.Read.WithContext(ctx).Raw(query, id, from, to, afterBucket, id).Rows()
		if err != nil {
			return err
		}
		batch, err := readTrendBatch(rows, id)
		if err != nil {
			return err
		}
		for _, sample := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(sample); err != nil {
				return err
			}
		}
		if len(batch) < 2048 {
			return nil
		}
		afterBucket = (batch[len(batch)-1].ObservedToMS + 59999) / 60000
	}
}

type trendBucket struct {
	first, last            *TrendPoint
	minEquity, maxEquity   *TrendPoint
	minBalance, maxBalance *TrendPoint
	firstGap, lastGap      *TrendPoint
}

type trendReducer struct {
	from, to     int64
	buckets      [300]trendBucket
	total, valid int
}

func newTrendReducer(from, to int64) *trendReducer { return &trendReducer{from: from, to: to} }

func (r *trendReducer) add(point TrendPoint) {
	point.index = r.total
	r.total++
	bucket := &r.buckets[min(299, max(0, int((point.TimeMS-r.from)*300/(r.to-r.from))))]
	p := &point
	if bucket.first == nil {
		bucket.first = p
	}
	bucket.last = p
	if p.Equity == nil {
		if bucket.firstGap == nil {
			bucket.firstGap = p
		}
		bucket.lastGap = p
		return
	}
	r.valid++
	if bucket.minEquity == nil || p.Equity.LessThan(*bucket.minEquity.Equity) {
		bucket.minEquity = p
	}
	if bucket.maxEquity == nil || p.Equity.GreaterThan(*bucket.maxEquity.Equity) {
		bucket.maxEquity = p
	}
	if bucket.minBalance == nil || p.Balance.LessThan(*bucket.minBalance.Balance) {
		bucket.minBalance = p
	}
	if bucket.maxBalance == nil || p.Balance.GreaterThan(*bucket.maxBalance.Balance) {
		bucket.maxBalance = p
	}
}

func (r *trendReducer) finish(view *TrendSeries) {
	selected := map[int]*TrendPoint{}
	for _, bucket := range r.buckets {
		for _, point := range []*TrendPoint{bucket.first, bucket.last, bucket.minEquity, bucket.maxEquity,
			bucket.minBalance, bucket.maxBalance, bucket.firstGap, bucket.lastGap} {
			if point != nil {
				selected[point.index] = point
			}
		}
	}
	indices := make([]int, 0, len(selected))
	for index := range selected {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		view.Points = append(view.Points, *selected[index])
	}
	view.TotalPoints, view.ValidPoints, view.GapPoints = r.total, r.valid, r.total-r.valid
	view.Downsampled = len(view.Points) < r.total
}
