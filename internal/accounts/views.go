package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/marketrest"
	"portexa/internal/storage"
)

type ServerView struct {
	storage.Server
	Endpoints         []storage.Endpoint `json:"endpoints"`
	TimeZone          string             `json:"time_zone"`
	TimeZoneVerified  bool               `json:"time_zone_verified"`
	TimeRulesetID     string             `json:"time_ruleset_id"`
	TimeMode          string             `json:"time_mode"`
	AutomaticTimeZone AutomaticTimeZone  `json:"automatic_time_zone"`
}

type PositionView struct {
	storage.Position
	NetProfit decimal.Decimal `json:"net_profit"`
	Lots      decimal.Decimal `json:"lots"`
}

type AccountView struct {
	ID                                 string            `json:"id"`
	ServerID                           string            `json:"server_id"`
	ServerName                         string            `json:"server_name"`
	BrokerName                         string            `json:"broker_name"`
	EndpointID                         string            `json:"endpoint_id"`
	BackupEndpointIDs                  []string          `json:"backup_endpoint_ids"`
	LastCollectionRevision             *int64            `json:"last_collection_revision"`
	EndpointApproved                   bool              `json:"endpoint_approved"`
	Protocol                           string            `json:"protocol"`
	Login                              uint64            `json:"login"`
	Label                              string            `json:"label"`
	ConfirmedCurrency                  *string           `json:"confirmed_currency"`
	UnitConfirmed                      bool              `json:"unit_confirmed"`
	ConfiguredUnitConfirmed            bool              `json:"configured_unit_confirmed"`
	ConfigVersion                      int64             `json:"config_version"`
	SyncEnabled                        bool              `json:"sync_enabled"`
	NextSyncAtMS                       int64             `json:"next_sync_at_ms"`
	SyncState                          string            `json:"sync_state"`
	Snapshot                           *storage.Snapshot `json:"snapshot"`
	LastRun                            *storage.SyncRun  `json:"last_run"`
	Stale                              bool              `json:"stale"`
	MarketRest                         bool              `json:"market_rest"`
	EffectiveCollectionIntervalSeconds int               `json:"effective_collection_interval_seconds"`
	CreatedAtMS                        int64             `json:"created_at_ms"`
	Daily                              DailyView         `json:"daily"`
	Funding                            FundingView       `json:"funding"`
}

type AccountDetail struct {
	Account          AccountView    `json:"account"`
	Positions        []PositionView `json:"positions"`
	ServerTimeStatus string         `json:"server_time_status"`
}

type MoneyTotals struct {
	Currency          string          `json:"currency"`
	ExpectedAccounts  int             `json:"expected_accounts"`
	IncludedAccounts  int             `json:"included_accounts"`
	StaleAccounts     int             `json:"stale_accounts"`
	Complete          bool            `json:"complete"`
	Balance           decimal.Decimal `json:"balance"`
	Credit            decimal.Decimal `json:"credit"`
	Equity            decimal.Decimal `json:"equity"`
	FloatingNetProfit decimal.Decimal `json:"floating_net_profit"`
	ObservedFromMS    *int64          `json:"observed_from_ms"`
	ObservedToMS      *int64          `json:"observed_to_ms"`
}

// Zero known accounts means unknown money, rather than a known zero balance.
func (total MoneyTotals) MarshalJSON() ([]byte, error) {
	type fields MoneyTotals
	var balance, credit, equity, profit *decimal.Decimal
	if total.IncludedAccounts > 0 {
		balance, credit, equity, profit = &total.Balance, &total.Credit, &total.Equity, &total.FloatingNetProfit
	}
	return json.Marshal(struct {
		fields
		Balance           *decimal.Decimal `json:"balance"`
		Credit            *decimal.Decimal `json:"credit"`
		Equity            *decimal.Decimal `json:"equity"`
		FloatingNetProfit *decimal.Decimal `json:"floating_net_profit"`
	}{fields(total), balance, credit, equity, profit})
}

type Overview struct {
	Accounts           []AccountView              `json:"accounts"`
	USDTotal           MoneyTotals                `json:"usd_total"`
	CurrencyTotals     []MoneyTotals              `json:"currency_totals"`
	AccountCount       int                        `json:"account_count"`
	MissingCount       int                        `json:"missing_count"`
	StaleCount         int                        `json:"stale_count"`
	ExcludedUSDCount   int                        `json:"excluded_usd_count"`
	KeyReady           bool                       `json:"key_ready"`
	AsOfMS             int64                      `json:"as_of_ms"`
	Maintenance        storage.Maintenance        `json:"maintenance"`
	CollectionSettings storage.CollectionSettings `json:"collection_settings"`
}

func (s *Service) Servers(ctx context.Context) ([]ServerView, error) {
	views := make([]ServerView, 0)
	err := s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var servers []storage.Server
		if err := tx.Order("created_at_ms, id").Find(&servers).Error; err != nil {
			return err
		}
		for _, server := range servers {
			automatic, err := automaticClockView(tx, server, s.Now())
			if err != nil {
				return err
			}
			view := ServerView{Server: server, Endpoints: make([]storage.Endpoint, 0), TimeMode: "unavailable",
				AutomaticTimeZone: automatic}
			if err := tx.Where("server_id = ?", server.ID).Order("revision DESC").Find(&view.Endpoints).Error; err != nil {
				return err
			}
			var rule serverTimeRule
			query := tx.Where("server_id = ?", server.ID)
			if server.ActiveTimeRulesetID != nil {
				view.TimeRulesetID = *server.ActiveTimeRulesetID
				query = query.Where("id = ?", *server.ActiveTimeRulesetID)
			}
			result := query.Order("revision DESC").Limit(1).Find(&rule)
			if result.Error != nil {
				return result.Error
			}
			if rule.RulesJSON != "" {
				var entries []timeRuleEntry
				if json.Unmarshal([]byte(rule.RulesJSON), &entries) == nil && len(entries) > 0 {
					view.TimeZone = entries[0].Zone
					_, label, err := serverLocation(entries[0], s.Now())
					if err == nil {
						view.TimeZone = label
						view.TimeZoneVerified = rule.VerifiedAtMS != nil && rule.ID == view.TimeRulesetID
						if entries[0].Kind == "observed_offset" {
							valid, err := measuredClockValid(tx, server, entries[0], s.Now())
							if err != nil {
								return err
							}
							view.TimeZoneVerified = view.TimeZoneVerified && valid
						}
						if view.TimeZoneVerified {
							view.TimeMode = "manual"
							if entries[0].Kind == "broker_us_dst" {
								view.TimeMode = "automatic"
							} else if entries[0].Kind == "observed_offset" {
								view.TimeMode = "calibrated"
							}
						}
					}
				}
			}
			views = append(views, view)
		}
		return nil
	})
	return views, err
}

func (s *Service) readView(tx *gorm.DB, account storage.Account, now int64, settings storage.CollectionSettings) (AccountView, error) {
	staleAfter := snapshotStaleAfter(settings)
	view := AccountView{ID: account.ID, ServerID: account.ServerID, EndpointID: account.PinnedEndpointID,
		BackupEndpointIDs: backupIDs(account),
		Login:             account.Login, Label: account.Label, ConfirmedCurrency: account.Currency, UnitConfirmed: account.UnitVerifiedAtMS != nil,
		ConfiguredUnitConfirmed: account.UnitVerifiedAtMS != nil,
		ConfigVersion:           account.ConfigVersion, SyncEnabled: account.SyncEnabled, NextSyncAtMS: account.NextSyncAtMS,
		CreatedAtMS: account.CreatedAtMS, SyncState: "idle"}
	view.EffectiveCollectionIntervalSeconds = settings.SnapshotIntervalSeconds
	var server storage.Server
	if err := tx.First(&server, "id = ?", account.ServerID).Error; err != nil {
		return view, err
	}
	view.ServerName, view.BrokerName, view.Protocol = server.ServerName, server.BrokerName, server.Protocol
	endpoints, err := s.collectionEndpoints(tx, account)
	if err != nil && !errors.Is(err, ErrEndpoint) && !errors.Is(err, ErrTarget) {
		return view, err
	}
	view.EndpointApproved = len(endpoints) > 0
	for _, endpoint := range endpoints {
		if endpoint.ID == account.LastCollectionEndpointID {
			view.LastCollectionRevision = &endpoint.Revision
			break
		}
	}
	if account.CurrentSnapshotID != nil {
		var snapshot storage.Snapshot
		if err := tx.First(&snapshot, "id = ? AND account_id = ?", *account.CurrentSnapshotID, account.ID).Error; err != nil {
			return view, err
		}
		active, err := restSnapshotTemplateActive(tx, account, snapshot, settings, time.UnixMilli(now))
		if err != nil {
			return view, err
		}
		if active {
			from, to, _, err := snapshotRestBounds(tx, snapshot, time.UnixMilli(now))
			if err != nil {
				return view, err
			}
			view.MarketRest = marketrest.ReducedWindow(time.UnixMilli(now), from, to)
			if view.MarketRest {
				view.EffectiveCollectionIntervalSeconds = max(settings.SnapshotIntervalSeconds, 300)
				staleAfter = max(staleAfter, marketrest.Interval+marketrest.Grace)
			} else {
				view.EffectiveCollectionIntervalSeconds = min(settings.SnapshotIntervalSeconds, 60)
				staleAfter = max(3*time.Minute, 3*time.Duration(view.EffectiveCollectionIntervalSeconds)*time.Second)
			}
		}
		view.EffectiveCollectionIntervalSeconds = int(dailyBoundaryInterval(time.UnixMilli(now), time.Duration(view.EffectiveCollectionIntervalSeconds)*time.Second) / time.Second)
		view.Stale = now-snapshot.ObservedToMS > staleAfter.Milliseconds()
		isUSC := account.Currency != nil && *account.Currency == "USC" && (snapshot.NativeCurrency == "USD" || snapshot.NativeCurrency == "USC")
		if account.Currency == nil || (*account.Currency != snapshot.NativeCurrency && !isUSC) || account.UnitRevision != snapshot.UnitRevision {
			view.UnitConfirmed = false
		}
		hideUnverifiedMT5Margin(view.Protocol, &snapshot)
		view.Snapshot = &snapshot
	}
	var run storage.SyncRun
	err = tx.Where("account_id = ?", account.ID).Order("started_at_ms DESC, id DESC").First(&run).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return view, err
	}
	if err == nil {
		view.LastRun, view.SyncState = &run, run.State
	}
	if !account.SyncEnabled {
		view.SyncState = "paused"
	} else if view.SyncState != "running" && account.NextSyncAtMS <= now {
		view.SyncState = "queued"
	}
	view.Daily, err = readDailyView(tx, account, server, time.UnixMilli(now))
	if err != nil {
		return view, err
	}
	view.Funding, err = readFundingView(tx, account, server, view.Snapshot, time.UnixMilli(now))
	return view, err
}

func (s *Service) List(ctx context.Context) ([]AccountView, error) {
	views, _, err := s.list(ctx)
	return views, err
}

func (s *Service) list(ctx context.Context) ([]AccountView, storage.CollectionSettings, error) {
	views := make([]AccountView, 0)
	var settings storage.CollectionSettings
	err := s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var list []storage.Account
		if err := tx.Where("removed_at_ms IS NULL").Order("created_at_ms, id").Find(&list).Error; err != nil {
			return err
		}
		var err error
		settings, err = readCollectionSettings(tx)
		if err != nil {
			return err
		}
		now := s.Now().UnixMilli()
		for _, account := range list {
			view, err := s.readView(tx, account, now, settings)
			if err != nil {
				return err
			}
			views = append(views, view)
		}
		return nil
	})
	return views, settings, err
}

func (s *Service) Detail(ctx context.Context, id, order string) (AccountDetail, error) {
	if order == "" {
		order = "ticket"
	}
	if order != "ticket" && order != "open_time_wall" {
		return AccountDetail{}, &InputError{"只支持订单号或服务器开仓时间排序"}
	}
	detail := AccountDetail{Positions: make([]PositionView, 0), ServerTimeStatus: "unverified"}
	err := s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account storage.Account
		if err := tx.First(&account, "id = ? AND removed_at_ms IS NULL", id).Error; err != nil {
			return err
		}
		settings, err := readCollectionSettings(tx)
		if err != nil {
			return err
		}
		detail.Account, err = s.readView(tx, account, s.Now().UnixMilli(), settings)
		if detail.Account.Daily.TimeZone != "" {
			detail.ServerTimeStatus = "verified"
		}
		if err != nil || account.CurrentSnapshotID == nil {
			return err
		}
		var positions []storage.Position
		sortOrder := "ticket ASC, command ASC"
		if order == "open_time_wall" {
			sortOrder = "open_time_wall ASC, " + sortOrder
		}
		if err = tx.Where("account_id = ? AND snapshot_id = ?", id, *account.CurrentSnapshotID).Order(sortOrder).Find(&positions).Error; err != nil {
			return err
		}
		for _, position := range positions {
			detail.Positions = append(detail.Positions, PositionView{Position: position,
				NetProfit: position.NetProfit(), Lots: position.Lots()})
		}
		return nil
	})
	return detail, err
}

func (s *Service) Snapshots(ctx context.Context, id string, before int64, limit int) ([]storage.Snapshot, error) {
	if limit < 1 || limit > 200 {
		return nil, &InputError{"查询条数需要在1至200之间"}
	}
	var server storage.Server
	read := s.Store.Read.WithContext(ctx)
	if err := read.Table("accounts AS a").Select("s.protocol").
		Joins("JOIN servers AS s ON s.id = a.server_id").
		Where("a.id = ? AND a.removed_at_ms IS NULL", id).Take(&server).Error; err != nil {
		return nil, err
	}
	query := read.Where("account_id = ?", id)
	if before > 0 {
		query = query.Where("observed_to_ms < ?", before)
	}
	list := make([]storage.Snapshot, 0)
	if err := query.Order("observed_to_ms DESC, id DESC").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	for i := range list {
		hideUnverifiedMT5Margin(server.Protocol, &list[i])
	}
	return list, nil
}

func hideUnverifiedMT5Margin(protocol string, snapshot *storage.Snapshot) {
	// Older native MT5 builds stored zero when the margin was missing.
	if protocol == "mt5" && snapshot.Margin != nil && snapshot.Margin.IsZero() {
		snapshot.Margin, snapshot.MarginAvailable = nil, false
	}
}

func addTotal(total *MoneyTotals, snapshot storage.Snapshot, scale decimal.Decimal, stale bool) {
	total.IncludedAccounts++
	if stale {
		total.StaleAccounts++
	}
	total.Balance = total.Balance.Add(snapshot.Balance.Mul(scale))
	total.Credit = total.Credit.Add(snapshot.Credit.Mul(scale))
	total.Equity = total.Equity.Add(snapshot.Equity.Mul(scale))
	total.FloatingNetProfit = total.FloatingNetProfit.Add(snapshot.FloatingNetProfit.Mul(scale))
	if total.ObservedFromMS == nil || snapshot.ObservedFromMS < *total.ObservedFromMS {
		value := snapshot.ObservedFromMS
		total.ObservedFromMS = &value
	}
	if total.ObservedToMS == nil || snapshot.ObservedToMS > *total.ObservedToMS {
		value := snapshot.ObservedToMS
		total.ObservedToMS = &value
	}
}

func (s *Service) Overview(ctx context.Context) (Overview, error) {
	list, settings, err := s.list(ctx)
	if err != nil {
		return Overview{}, err
	}
	view := Overview{Accounts: list, USDTotal: MoneyTotals{Currency: "USD"}, CurrencyTotals: make([]MoneyTotals, 0), AccountCount: len(list), KeyReady: s.Vault.Ready(), AsOfMS: s.Now().UnixMilli()}
	view.CollectionSettings = settings
	view.Maintenance = s.Store.Maintenance()
	raw := map[string]*MoneyTotals{}
	for _, account := range list {
		currency := ""
		if account.ConfirmedCurrency != nil {
			currency = *account.ConfirmedCurrency
		}
		if account.Snapshot != nil {
			currency = account.Snapshot.NativeCurrency
		} else {
			view.MissingCount++
		}
		if account.Stale {
			view.StaleCount++
		}
		if currency != "" {
			if raw[currency] == nil {
				raw[currency] = &MoneyTotals{Currency: currency}
			}
			raw[currency].ExpectedAccounts++
			if account.Snapshot != nil {
				addTotal(raw[currency], *account.Snapshot, decimal.NewFromInt(1), account.Stale)
			}
		}
		eligible := account.ConfirmedCurrency != nil && (*account.ConfirmedCurrency == "USD" || *account.ConfirmedCurrency == "USC") && account.UnitConfirmed
		if eligible {
			view.USDTotal.ExpectedAccounts++
			if account.Snapshot != nil && account.Snapshot.USDScale != nil {
				addTotal(&view.USDTotal, *account.Snapshot, *account.Snapshot.USDScale, account.Stale)
			}
		} else {
			view.ExcludedUSDCount++
		}
	}
	view.USDTotal.Complete = view.USDTotal.ExpectedAccounts > 0 && view.USDTotal.ExpectedAccounts == view.USDTotal.IncludedAccounts
	for _, total := range raw {
		total.Complete = total.ExpectedAccounts == total.IncludedAccounts
		view.CurrencyTotals = append(view.CurrencyTotals, *total)
	}
	sort.Slice(view.CurrencyTotals, func(i, j int) bool { return view.CurrencyTotals[i].Currency < view.CurrencyTotals[j].Currency })
	return view, nil
}
