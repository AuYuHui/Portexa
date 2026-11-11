package main

import (
	"context"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/mt4"
	"portexa/tests/testutil"
)

// Exercise the real publication/calibration path with synthetic events. No
// broker rule, manual timezone or production SDK connection is involved.
func seedTradeClock(ctx context.Context, service *accounts.Service) error {
	var account storage.Account
	if err := service.Store.Read.First(&account, "login = ?", 100102).Error; err != nil {
		return err
	}
	defer func() { service.Now = time.Now }()
	finish := time.Now().Truncate(time.Second)
	openWall := uint32(finish.Add(-time.Hour).Unix() + 345*60)
	trades := []mt4.Trade{
		{Ticket: 81001, Command: 0, Symbol: "SYNTHUSD", OpenTime: openWall, Digits: 2, VolumeHundredths: 4},
		{Ticket: 81002, Command: 1, Symbol: "SYNTHUSD", OpenTime: openWall, Digits: 2, VolumeHundredths: 4},
		{Ticket: 81003, Command: 0, Symbol: "SYNTHUSD", OpenTime: openWall, Digits: 2, VolumeHundredths: 4},
	}
	for phase := 0; phase < 3; phase++ {
		stamp := finish.Add(time.Duration(phase-2) * time.Minute)
		service.Now = func() time.Time { return stamp }
		if err := service.RequestSync(ctx, account.ID); err != nil {
			return err
		}
		job, err := service.Claim(ctx, account.ID)
		if err != nil {
			return err
		}
		result := testutil.Result("USC")
		switch phase {
		case 0:
			result.Trades = trades
		case 1:
			result.Trades = trades[2:]
		case 2:
			result.Trades = nil
		}
		snapshot, positions, err := syncer.Convert(job, result, stamp.Add(-time.Second).UnixMilli(), stamp.UnixMilli())
		if err != nil {
			return err
		}
		if err := service.Publish(ctx, job, snapshot, positions, finish.Add(time.Hour).UnixMilli()); err != nil {
			return err
		}
		if phase == 0 {
			continue
		}
		clock, err := service.ClaimClock(ctx, account.ID)
		if err != nil {
			return err
		}
		history := append([]mt4.Trade(nil), trades[:2]...)
		if phase == 2 {
			history = append([]mt4.Trade(nil), trades[2:]...)
		}
		for index := range history {
			history[index].CloseTime = uint32(stamp.Add(-30*time.Second).Unix() + 345*60)
		}
		result.HistoryRead, result.HistoryComplete = true, true
		result.HistoryCovered = []mt4.HistoryRange{{Start: clock.Run.WindowStart, End: clock.Run.WindowEnd}}
		result.History = history
		closed, err := syncer.ConvertClock(clock, result)
		if err != nil {
			return err
		}
		if err := service.PublishClock(ctx, clock, closed, stamp.UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}
