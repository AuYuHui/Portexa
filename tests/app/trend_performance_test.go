package app_test

import (
	"testing"
	"time"

	"gorm.io/gorm"
	"portexa/internal/storage"
	"portexa/tests/testutil"
)

// Explicit benchmark, excluded from ordinary unit-test runs. The source count
// is the 90-day plan volume at 30 seconds, not a production capacity promise.
func BenchmarkAssetTrendNinetyDays(b *testing.B) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	now := from
	f := testutil.New(b, false)
	f.Accounts.Now = func() time.Time { return now }
	server, endpoint := f.Server(b, "Synthetic-Trend-Ninety-Days")
	a := f.Account(b, server, endpoint, 9701, "USD")
	const sourceCount = 90 * 24 * 60 * 2
	seedTrendSamples(b, f, a, from, sourceCount)
	if err := f.Store.Write(b.Context(), storage.Low, func(tx *gorm.DB) error {
		return tx.Model(&a).Update("included_at_ms", from.UnixMilli()).Error
	}); err != nil {
		b.Fatal(err)
	}
	now = from.Add(90 * 24 * time.Hour)
	input := trendRange(from, now, "native")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		started := time.Now()
		view, err := f.Accounts.AccountTrend(b.Context(), a.ID, input)
		if err != nil || view.TotalPoints != sourceCount || len(view.Points) > 2400 || !view.Downsampled {
			b.Fatal("90-day query failed", view.TotalPoints, len(view.Points), err)
		}
		b.ReportMetric(float64(time.Since(started).Microseconds())/1000, "account_ms")
		started = time.Now()
		input.Unit = "USD"
		portfolio, err := f.Accounts.PortfolioTrend(b.Context(), input)
		if err != nil || portfolio.ValidPoints != sourceCount/2 || len(portfolio.Points) > 2400 {
			b.Fatal("90-day portfolio query failed", portfolio.ValidPoints, len(portfolio.Points), err)
		}
		b.ReportMetric(float64(time.Since(started).Microseconds())/1000, "portfolio_ms")
		input.Unit = "native"
	}
	b.ReportMetric(sourceCount, "source_rows/op")
}
