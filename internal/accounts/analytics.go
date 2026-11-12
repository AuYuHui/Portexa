package accounts

import (
	"context"
	"errors"
	"sort"
	"time"

	"portexa/internal/analytics"
	"portexa/internal/storage"
)

var ErrAnalyticsChanged = errors.New("分析依据已更新，请刷新后重试")

type AnalyticsRequest struct {
	FromMS    int64
	ToMS      int64
	AccountID string
	Unit      string
}

type AnalyticsReport struct {
	Result             analytics.Result   `json:"result"`
	Accounts           []analytics.Result `json:"accounts"`
	Events             []analytics.Event  `json:"events"`
	Transfers          []analytics.Event  `json:"unmatched_transfers"`
	RequestedFromMS    int64              `json:"requested_from_ms"`
	RequestedToMS      int64              `json:"requested_to_ms"`
	GeneratedAtMS      int64              `json:"generated_at_ms"`
	SourceRevision     int64              `json:"source_revision"`
	CalculationVersion string             `json:"calculation_version"`
	Method             string             `json:"method"`
	Timezone           string             `json:"timezone"`
	SamplingMS         int64              `json:"sampling_ms"`
	PendingAccounts    int                `json:"pending_accounts"`
	ExcludedAccounts   int                `json:"excluded_accounts"`
	EventsTruncated    bool               `json:"events_truncated"`
	Stale              bool               `json:"stale"`
}

func (s *Service) validateAnalytics(input AnalyticsRequest) (AnalyticsRequest, error) {
	now := s.Now().UnixMilli()
	if input.FromMS <= 0 || input.ToMS <= input.FromMS || input.ToMS-input.FromMS > analytics.MaxRange || input.ToMS > now+analytics.Minute ||
		(input.Unit != "USD" && input.Unit != "native") || input.AccountID == "" && input.Unit != "USD" {
		return input, &InputError{"分析时间范围或单位无效，最多查询90天"}
	}
	input.FromMS = (input.FromMS + analytics.Minute - 1) / analytics.Minute * analytics.Minute
	input.ToMS = min(input.ToMS, now) / analytics.Minute * analytics.Minute
	if input.ToMS-input.FromMS < analytics.Minute {
		return input, &InputError{"分析区间至少需要两个相邻分钟时点"}
	}
	return input, nil
}

func (s *Service) Analytics(ctx context.Context, requested AnalyticsRequest) (AnalyticsReport, error) {
	view := AnalyticsReport{Accounts: []analytics.Result{}, Events: []analytics.Event{}, Transfers: []analytics.Event{},
		RequestedFromMS: requested.FromMS, RequestedToMS: requested.ToMS, GeneratedAtMS: s.Now().UnixMilli(),
		CalculationVersion: analytics.Version, Method: "linked_modified_dietz", Timezone: "Asia/Shanghai", SamplingMS: analytics.Minute}
	input, err := s.validateAnalytics(requested)
	if err != nil {
		return view, err
	}
	view.SourceRevision, err = analyticsRevision(s.Store.Read.WithContext(ctx))
	if err != nil {
		return view, err
	}
	var list []storage.Account
	query := s.Store.Read.WithContext(ctx).Order("id")
	if input.AccountID != "" {
		query = query.Where("id=?", input.AccountID)
	} else {
		query = query.Where("created_at_ms<? AND (removed_at_ms IS NULL OR removed_at_ms>=?)", input.ToMS, input.FromMS)
	}
	if err := query.Find(&list).Error; err != nil {
		return view, err
	}
	if input.AccountID != "" && len(list) == 0 {
		return view, storage.ErrNotFound
	}
	sources := []*analyticsSource{}
	cutoff := input.ToMS
	readOrders := 0
	for _, account := range list {
		source, err := s.analyticsSource(ctx, account, input.FromMS, input.ToMS, input.Unit)
		if err != nil {
			return view, err
		}
		sources = append(sources, source)
		readOrders += source.readOrders
		if readOrders > 200_000 {
			return view, &InputError{"分析区间交易过多，请缩短时间范围"}
		}
		if input.AccountID != "" || account.USDScale != nil && account.IncludedAtMS != nil {
			if source.cutoff >= input.FromMS+analytics.Minute {
				cutoff = min(cutoff, source.cutoff)
			}
		}
	}
	portfolio := newAnalyticsPortfolio(input.FromMS, cutoff)
	for _, source := range sources {
		if err := s.analyticsSamples(ctx, source, input.FromMS, cutoff, input.Unit); err != nil {
			return view, err
		}
		result, err := analytics.Calculate(ctx, source.input, input.AccountID != "")
		if err != nil {
			return view, err
		}
		if input.AccountID != "" {
			view.Result = result
		} else {
			view.Accounts = append(view.Accounts, result)
			if source.account.USDScale == nil {
				view.ExcludedAccounts++
			} else if source.account.IncludedAtMS == nil {
				view.PendingAccounts++
			} else {
				if err := s.addAnalyticsPortfolio(ctx, portfolio, source); err != nil {
					return view, err
				}
			}
		}
		for _, event := range source.input.Events {
			if event.TimeMS < input.FromMS || event.TimeMS >= cutoff || event.Kind == "trade" || event.Kind == "credit" {
				continue
			}
			if len(view.Events) < 200 {
				view.Events = append(view.Events, event)
			} else {
				view.EventsTruncated = true
			}
		}
		source.input.Samples, source.input.Coverage = nil, nil
	}
	if input.AccountID == "" {
		if err := s.finishAnalyticsPortfolio(ctx, portfolio, sources, &view); err != nil {
			return view, err
		}
		view.Result, err = analytics.Calculate(ctx, portfolio.input, true)
		if err != nil {
			return view, err
		}
	}
	current, err := analyticsRevision(s.Store.Read.WithContext(ctx))
	if err != nil {
		return view, err
	}
	if current != view.SourceRevision {
		return view, ErrAnalyticsChanged
	}
	view.Stale = cutoff < view.GeneratedAtMS-(10*time.Minute).Milliseconds()
	sort.SliceStable(view.Accounts, func(i, j int) bool {
		a, b := view.Accounts[i].Metrics.ReturnPct.Value, view.Accounts[j].Metrics.ReturnPct.Value
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.GreaterThan(*b)
	})
	sort.Slice(view.Events, func(i, j int) bool { return view.Events[i].TimeMS < view.Events[j].TimeMS })
	return view, nil
}

type AnalyticsActivity struct {
	Items    []analytics.Event `json:"items"`
	Total    int               `json:"total"`
	Offset   int               `json:"offset"`
	Currency string            `json:"currency"`
}

func (s *Service) AnalyticsActivity(ctx context.Context, requested AnalyticsRequest, offset int) (AnalyticsActivity, error) {
	view := AnalyticsActivity{Items: []analytics.Event{}, Offset: offset, Currency: "USD"}
	input, err := s.validateAnalytics(requested)
	if err != nil {
		return view, err
	}
	if input.ToMS-input.FromMS > 24*60*analytics.Minute || offset < 0 || offset > 200_000 {
		return view, &InputError{"明细查询限一天，分页参数无效"}
	}
	revision, err := analyticsRevision(s.Store.Read.WithContext(ctx))
	if err != nil {
		return view, err
	}
	var list []storage.Account
	query := s.Store.Read.WithContext(ctx).Order("id")
	if input.AccountID != "" {
		query = query.Where("id=?", input.AccountID)
	} else {
		query = query.Where("usd_scale IS NOT NULL AND included_at_ms<?", input.ToMS)
	}
	if err := query.Find(&list).Error; err != nil {
		return view, err
	}
	if input.AccountID != "" && len(list) == 0 {
		return view, storage.ErrNotFound
	}
	all := []analytics.Event{}
	for _, account := range list {
		source, err := s.analyticsSource(ctx, account, input.FromMS, input.ToMS, input.Unit)
		if err != nil {
			return view, err
		}
		view.Currency = source.input.Currency
		for _, event := range source.input.Events {
			if event.TimeMS >= input.FromMS && event.TimeMS < input.ToMS && (input.AccountID != "" || analyticsMember(account, event.TimeMS)) {
				all = append(all, event)
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].TimeMS != all[j].TimeMS {
			return all[i].TimeMS < all[j].TimeMS
		}
		if all[i].AccountID != all[j].AccountID {
			return all[i].AccountID < all[j].AccountID
		}
		return all[i].Ticket < all[j].Ticket
	})
	view.Total = len(all)
	view.Items = all[min(offset, len(all)):min(offset+50, len(all))]
	current, err := analyticsRevision(s.Store.Read.WithContext(ctx))
	if err != nil {
		return view, err
	}
	if current != revision {
		return view, ErrAnalyticsChanged
	}
	return view, nil
}
