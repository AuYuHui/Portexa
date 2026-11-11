package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"portexa/internal/storage"
)

type ServerView struct {
	storage.Server
	Endpoints        []storage.Endpoint `json:"endpoints"`
	TimeZone         string             `json:"time_zone"`
	TimeZoneVerified bool               `json:"time_zone_verified"`
}

type PositionView struct {
	storage.Position
	NetProfit decimal.Decimal `json:"net_profit"`
	Lots      decimal.Decimal `json:"lots"`
}

type AccountView struct {
	ID                      string            `json:"id"`
	ServerID                string            `json:"server_id"`
	ServerName              string            `json:"server_name"`
	BrokerName              string            `json:"broker_name"`
	EndpointID              string            `json:"endpoint_id"`
	EndpointApproved        bool              `json:"endpoint_approved"`
	Login                   uint32            `json:"login"`
	Label                   string            `json:"label"`
	ConfirmedCurrency       *string           `json:"confirmed_currency"`
	UnitConfirmed           bool              `json:"unit_confirmed"`
	ConfiguredUnitConfirmed bool              `json:"configured_unit_confirmed"`
	ConfigVersion           int64             `json:"config_version"`
	SyncEnabled             bool              `json:"sync_enabled"`
	NextSyncAtMS            int64             `json:"next_sync_at_ms"`
	SyncState               string            `json:"sync_state"`
	Snapshot                *storage.Snapshot `json:"snapshot"`
	LastRun                 *storage.SyncRun  `json:"last_run"`
	Stale                   bool              `json:"stale"`
	CreatedAtMS             int64             `json:"created_at_ms"`
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
	Accounts         []AccountView       `json:"accounts"`
	USDTotal         MoneyTotals         `json:"usd_total"`
	CurrencyTotals   []MoneyTotals       `json:"currency_totals"`
	AccountCount     int                 `json:"account_count"`
	MissingCount     int                 `json:"missing_count"`
	StaleCount       int                 `json:"stale_count"`
	ExcludedUSDCount int                 `json:"excluded_usd_count"`
	KeyReady         bool                `json:"key_ready"`
	AsOfMS           int64               `json:"as_of_ms"`
	Maintenance      storage.Maintenance `json:"maintenance"`
}

func (s *Service) Servers(ctx context.Context) ([]ServerView, error) {
	views := make([]ServerView, 0)
	err := s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var servers []storage.Server
		if err := tx.Order("created_at_ms, id").Find(&servers).Error; err != nil {
			return err
		}
		for _, server := range servers {
			view := ServerView{Server: server, Endpoints: make([]storage.Endpoint, 0)}
			if err := tx.Where("server_id = ?", server.ID).Order("revision DESC").Find(&view.Endpoints).Error; err != nil {
				return err
			}
			var rule struct {
				RulesJSON    string
				VerifiedAtMS *int64
			}
			result := tx.Table("server_time_rule_sets").Where("server_id = ?", server.ID).Order("revision DESC").Limit(1).Find(&rule)
			if result.Error != nil {
				return result.Error
			}
			if rule.RulesJSON != "" {
				var entries []struct {
					Zone string `json:"zone"`
				}
				if json.Unmarshal([]byte(rule.RulesJSON), &entries) == nil && len(entries) > 0 {
					view.TimeZone = entries[0].Zone
				}
				view.TimeZoneVerified = rule.VerifiedAtMS != nil && server.ActiveTimeRulesetID != nil
			}
			views = append(views, view)
		}
		return nil
	})
	return views, err
}

func readView(tx *gorm.DB, account storage.Account, now int64) (AccountView, error) {
	view := AccountView{ID: account.ID, ServerID: account.ServerID, EndpointID: account.PinnedEndpointID,
		Login: account.Login, Label: account.Label, ConfirmedCurrency: account.Currency, UnitConfirmed: account.UnitVerifiedAtMS != nil,
		ConfiguredUnitConfirmed: account.UnitVerifiedAtMS != nil,
		ConfigVersion:           account.ConfigVersion, SyncEnabled: account.SyncEnabled, NextSyncAtMS: account.NextSyncAtMS,
		CreatedAtMS: account.CreatedAtMS, SyncState: "idle"}
	var server storage.Server
	if err := tx.First(&server, "id = ?", account.ServerID).Error; err != nil {
		return view, err
	}
	view.ServerName, view.BrokerName = server.ServerName, server.BrokerName
	var endpoint storage.Endpoint
	if err := tx.First(&endpoint, "id = ?", account.PinnedEndpointID).Error; err != nil {
		return view, err
	}
	view.EndpointApproved = endpoint.Approved()
	if account.CurrentSnapshotID != nil {
		var snapshot storage.Snapshot
		if err := tx.First(&snapshot, "id = ? AND account_id = ?", *account.CurrentSnapshotID, account.ID).Error; err != nil {
			return view, err
		}
		view.Snapshot = &snapshot
		view.Stale = now-snapshot.ObservedToMS > (3 * time.Minute).Milliseconds()
		if account.Currency == nil || *account.Currency != snapshot.NativeCurrency || account.UnitRevision != snapshot.UnitRevision {
			view.UnitConfirmed = false
		}
	}
	var run storage.SyncRun
	err := tx.Where("account_id = ?", account.ID).Order("started_at_ms DESC, id DESC").First(&run).Error
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
	return view, nil
}

func (s *Service) List(ctx context.Context) ([]AccountView, error) {
	views := make([]AccountView, 0)
	err := s.Store.Read.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var list []storage.Account
		if err := tx.Where("removed_at_ms IS NULL").Order("created_at_ms, id").Find(&list).Error; err != nil {
			return err
		}
		now := s.Now().UnixMilli()
		for _, account := range list {
			view, err := readView(tx, account, now)
			if err != nil {
				return err
			}
			views = append(views, view)
		}
		return nil
	})
	return views, err
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
		var err error
		detail.Account, err = readView(tx, account, s.Now().UnixMilli())
		if err != nil || account.CurrentSnapshotID == nil {
			return err
		}
		var positions []storage.Position
		if err = tx.Where("account_id = ? AND snapshot_id = ?", id, *account.CurrentSnapshotID).Order(order + " ASC").Find(&positions).Error; err != nil {
			return err
		}
		for _, position := range positions {
			detail.Positions = append(detail.Positions, PositionView{Position: position,
				NetProfit: position.NetProfit(), Lots: decimal.NewFromInt(int64(position.VolumeHundredths)).Div(decimal.NewFromInt(100))})
		}
		return nil
	})
	return detail, err
}

func (s *Service) Snapshots(ctx context.Context, id string, before int64, limit int) ([]storage.Snapshot, error) {
	if limit < 1 || limit > 200 {
		return nil, &InputError{"查询条数需要在1至200之间"}
	}
	var account storage.Account
	if err := s.Store.Read.WithContext(ctx).First(&account, "id = ?", id).Error; err != nil {
		return nil, err
	}
	query := s.Store.Read.WithContext(ctx).Where("account_id = ?", id)
	if before > 0 {
		query = query.Where("observed_to_ms < ?", before)
	}
	list := make([]storage.Snapshot, 0)
	err := query.Order("observed_to_ms DESC, id DESC").Limit(limit).Find(&list).Error
	return list, err
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
	list, err := s.List(ctx)
	if err != nil {
		return Overview{}, err
	}
	view := Overview{Accounts: list, USDTotal: MoneyTotals{Currency: "USD"}, CurrencyTotals: make([]MoneyTotals, 0), AccountCount: len(list), KeyReady: s.Vault.Ready(), AsOfMS: s.Now().UnixMilli()}
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
