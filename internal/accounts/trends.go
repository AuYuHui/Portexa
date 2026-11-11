package accounts

import (
	"context"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"portexa/internal/storage"
)

const TrendMaxRange = 90 * 24 * time.Hour
const trendAlignmentMS int64 = 60_000
const trendMaxAgeMS int64 = 90_000

type TrendRequest struct {
	FromMS int64
	ToMS   int64
	Unit   string
}

type TrendPoint struct {
	TimeMS            int64            `json:"time_ms"`
	ObservedFromMS    int64            `json:"observed_from_ms"`
	ObservedToMS      int64            `json:"observed_to_ms"`
	SnapshotID        string           `json:"snapshot_id,omitempty"`
	Currency          string           `json:"currency"`
	UnitRevision      int64            `json:"unit_revision"`
	Balance           *decimal.Decimal `json:"balance"`
	Equity            *decimal.Decimal `json:"equity"`
	Credit            *decimal.Decimal `json:"credit"`
	FloatingNetProfit *decimal.Decimal `json:"floating_net_profit"`
	Segment           int64            `json:"segment"`
	Reason            string           `json:"reason"`
	ExpectedAccounts  int              `json:"expected_accounts"`
	IncludedAccounts  int              `json:"included_accounts"`
	CadenceKnown      bool             `json:"cadence_known"`
	index             int
}

type TrendEvent struct {
	TimeMS    int64            `json:"time_ms"`
	Kind      string           `json:"kind"`
	AccountID string           `json:"account_id"`
	Ticket    int32            `json:"ticket,omitempty"`
	Amount    *decimal.Decimal `json:"amount"`
	Currency  string           `json:"currency"`
}

type TrendSeries struct {
	Scope            string       `json:"scope"`
	Unit             string       `json:"unit"`
	NativeCurrency   string       `json:"native_currency"`
	Currencies       []string     `json:"currencies"`
	FromMS           int64        `json:"from_ms"`
	ToMS             int64        `json:"to_ms"`
	AsOfMS           int64        `json:"as_of_ms"`
	Points           []TrendPoint `json:"points"`
	Events           []TrendEvent `json:"events"`
	TotalPoints      int          `json:"total_points"`
	ValidPoints      int          `json:"valid_points"`
	GapPoints        int          `json:"gap_points"`
	BreakCount       int          `json:"break_count"`
	CanConvertUSD    bool         `json:"can_convert_usd"`
	UnknownCadence   int          `json:"unknown_cadence"`
	Downsampled      bool         `json:"downsampled"`
	SamplingMethod   string       `json:"sampling_method"`
	AlignmentMS      int64        `json:"alignment_ms"`
	MaxSampleAgeMS   int64        `json:"max_sample_age_ms"`
	ExcludedAccounts int          `json:"excluded_accounts"`
	PendingAccounts  int          `json:"pending_accounts"`
	UnplacedEvents   int          `json:"unplaced_events"`
	EventsTruncated  bool         `json:"events_truncated"`
	LastObservedToMS int64        `json:"last_observed_to_ms"`
	Stale            bool         `json:"stale"`
}

func (s *Service) validateTrend(input TrendRequest) (TrendSeries, error) {
	view := TrendSeries{Unit: input.Unit, FromMS: input.FromMS, ToMS: input.ToMS, AsOfMS: s.Now().UnixMilli(),
		Points: []TrendPoint{}, Events: []TrendEvent{}, Currencies: []string{}, SamplingMethod: "bucket_first_last_extrema"}
	if input.FromMS < 0 || input.ToMS <= input.FromMS || input.ToMS-input.FromMS > TrendMaxRange.Milliseconds() ||
		input.ToMS > view.AsOfMS+60_000 || (input.Unit != "native" && input.Unit != "USD") {
		return view, &InputError{"走势时间范围或单位无效，最多查询90天"}
	}
	return view, nil
}

func (s *Service) AccountTrend(ctx context.Context, id string, input TrendRequest) (TrendSeries, error) {
	view, err := s.validateTrend(input)
	view.Scope = "account"
	if err != nil {
		return view, err
	}
	var account storage.Account
	if err := s.Store.Read.WithContext(ctx).First(&account, "id=? AND removed_at_ms IS NULL", id).Error; err != nil {
		return view, err
	}
	reducer := newTrendReducer(input.FromMS, input.ToMS)
	var previous *storage.Snapshot
	segment := int64(0)
	currencies := map[string]bool{}
	err = s.visitTrendSnapshots(ctx, id, input.FromMS, input.ToMS, func(snapshot storage.Snapshot) error {
		p := snapshotTrendPoint(snapshot, input.Unit)
		if previous == nil || !continuousSnapshots(*previous, snapshot) {
			segment++
			if previous != nil {
				view.BreakCount++
			}
		}
		view.CanConvertUSD = view.CanConvertUSD || snapshot.USDScale != nil
		p.Segment = segment
		if !p.CadenceKnown {
			view.UnknownCadence++
		}
		currencies[snapshot.NativeCurrency] = true
		view.NativeCurrency = snapshot.NativeCurrency
		view.LastObservedToMS = snapshot.ObservedToMS
		reducer.add(p)
		previous = &snapshot
		return nil
	})
	if err != nil {
		return view, err
	}
	for currency := range currencies {
		view.Currencies = append(view.Currencies, currency)
	}
	sort.Strings(view.Currencies)
	reducer.finish(&view)
	settings, err := s.CollectionSettings(ctx)
	if err != nil {
		return view, err
	}
	view.Stale = view.LastObservedToMS == 0 || min(view.ToMS, view.AsOfMS)-view.LastObservedToMS > snapshotStaleAfter(settings).Milliseconds()
	if err := s.trendFundEvents(ctx, []storage.Account{account}, input, &view); err != nil {
		return view, err
	}
	return view, nil
}

func snapshotTrendPoint(snapshot storage.Snapshot, unit string) TrendPoint {
	p := TrendPoint{TimeMS: snapshot.ObservedToMS, ObservedFromMS: snapshot.ObservedFromMS, ObservedToMS: snapshot.ObservedToMS,
		SnapshotID: snapshot.ID, Currency: snapshot.NativeCurrency, UnitRevision: snapshot.UnitRevision,
		ExpectedAccounts: 1, IncludedAccounts: 1, CadenceKnown: snapshot.CollectionIntervalSeconds != nil}
	scale := decimal.NewFromInt(1)
	if unit == "USD" {
		p.Currency = "USD"
		if snapshot.USDScale == nil {
			p.Reason, p.IncludedAccounts = "unit_unconfirmed", 0
			return p
		}
		scale = *snapshot.USDScale
	}
	balance, equity := snapshot.Balance, snapshot.Equity
	credit, floating := snapshot.Credit, snapshot.FloatingNetProfit
	if !scale.Equal(decimal.NewFromInt(1)) {
		balance, equity = balance.Mul(scale), equity.Mul(scale)
		credit, floating = credit.Mul(scale), floating.Mul(scale)
	}
	p.Balance, p.Equity, p.Credit, p.FloatingNetProfit = &balance, &equity, &credit, &floating
	return p
}

func continuousSnapshots(a, b storage.Snapshot) bool {
	if a.CollectionIntervalSeconds == nil || b.CollectionIntervalSeconds == nil || a.UnitRevision != b.UnitRevision ||
		a.NativeCurrency != b.NativeCurrency || b.ObservedToMS < a.ObservedToMS {
		return false
	}
	threshold := max(180_000, 3*int64(max(*a.CollectionIntervalSeconds, *b.CollectionIntervalSeconds))*1000)
	return b.ObservedToMS-a.ObservedToMS <= threshold
}
