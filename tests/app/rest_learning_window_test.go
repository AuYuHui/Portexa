package app_test

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
)

func TestRestLearningNarrowWindowNeverFallsBackToWiderTemplate(t *testing.T) {
	f, a, now, start := restFixture(t)
	ny, _ := time.LoadLocation("America/New_York")
	friday := start.In(ny).AddDate(0, 0, -1)
	*now = time.Date(friday.Year(), friday.Month(), friday.Day(), 18, 0, 0, 0, ny)
	restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	var group storage.MarketLearningGroup
	if err := f.Store.Read.First(&group).Error; err != nil {
		t.Fatal(err)
	}
	model := storage.MarketLearningModel{ID: strings.Repeat("a", 64), GroupID: group.ID, Algorithm: "synthetic-narrow-window", CreatedAtMS: now.UnixMilli(), TrainedThroughMS: now.AddDate(0, 0, -7).UnixMilli(), ExpiresAtMS: now.AddDate(0, 0, 56).UnixMilli(), CloseMinute: 19 * 60, OpenMinute: 2*1440 + 15*60, TrainingKeysJSON: "[]", EvidenceSHA256: strings.Repeat("b", 64)}
	if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		if err := tx.Create(&storage.MarketLearningEvent{ModelID: model.ID, State: "adopted", Reason: "synthetic-validation", EffectiveAtMS: now.UnixMilli()}).Error; err != nil {
			return err
		}
		return tx.Model(&group).Update("active_model_id", model.ID).Error
	}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(30 * time.Minute)
	outside := restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	if outside.RestWindowID != "" || outside.RestVerified {
		t.Fatal("fell back to wider template before learned close")
	}
	*now = now.Add(36 * time.Minute)
	restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	*now = now.Add(time.Minute)
	inside := restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	if !inside.RestVerified || *inside.CollectionIntervalSeconds != 300 {
		t.Fatal("did not enter narrow learned window")
	}
	*now = time.Date(friday.Year(), friday.Month(), friday.Day()+2, 15, 1, 0, 0, ny)
	outside = restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	if outside.RestWindowID != "" || outside.RestVerified {
		t.Fatal("fell back to wider template after learned open")
	}
}

func TestRestLearningUnobservableQuoteKeepsTemplate(t *testing.T) {
	f, a, now, start := restFixture(t)
	ny, _ := time.LoadLocation("America/New_York")
	friday := start.In(ny).AddDate(0, 0, -1)
	*now = time.Date(friday.Year(), friday.Month(), friday.Day(), 17, 0, 0, 0, ny)
	restSnapshot(t, f, a, *now, "XAUUSD.c", 10)
	var group storage.MarketLearningGroup
	f.Store.Read.First(&group)
	model := storage.MarketLearningModel{ID: strings.Repeat("c", 64), GroupID: group.ID, Algorithm: "synthetic-wide-window", CreatedAtMS: now.UnixMilli(), TrainedThroughMS: now.AddDate(0, 0, -7).UnixMilli(), ExpiresAtMS: now.AddDate(0, 0, 56).UnixMilli(), CloseMinute: 17 * 60, OpenMinute: 2*1440 + 17*60, TrainingKeysJSON: "[]", EvidenceSHA256: strings.Repeat("d", 64)}
	if err := f.Store.Write(t.Context(), storage.Low, func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		if err := tx.Create(&storage.MarketLearningEvent{ModelID: model.ID, State: "adopted", Reason: "synthetic-validation", EffectiveAtMS: now.UnixMilli()}).Error; err != nil {
			return err
		}
		return tx.Model(&group).Update("active_model_id", model.ID).Error
	}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(30 * time.Minute)
	if err := f.Accounts.RequestSync(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	job, err := f.Accounts.Claim(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	result := mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Currency: "USD", Balance: 1000, Equity: 1010}, Trades: []mt4.Trade{{Ticket: 78301, Command: 0, Symbol: "XAUUSD.c", VolumeHundredths: 10, Profit: 10, OpenPrice: 1, ClosePrice: 0}}}
	sample, holdings, err := syncer.Convert(job, result, now.Add(-time.Second).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Accounts.PublishScheduled(t.Context(), job, sample, holdings); err != nil {
		t.Fatal(err)
	}
	f.Store.Read.First(&sample, "id=?", sample.ID)
	if sample.RestWindowID != "" || sample.RestVerified {
		t.Fatal("applied a learned extension with no observable quote")
	}
}
