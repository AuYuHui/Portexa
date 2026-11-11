package client_test

import (
	"context"
	"testing"

	"portexa/mt4"
	"portexa/mt4/tests/support"
)

func TestHistoryCommentIsPreservedThroughPublicFetch(t *testing.T) {
	for _, comment := range []string{"Deposit #synthetic", "入金", "12345678901234567890123456789012", "D(DIN100000000000000001)", "W(DOUT100000000000000002)"} {
		t.Run(comment, func(t *testing.T) {
			record := support.TradeBytes(11, 1700000008, 10)
			copy(record[0x94:0xb4], comment)
			record[0xb4] = '!'
			address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true, HistoryPages: []support.HistoryPage{{Records: record}}})
			result, err := mt4.Fetch(context.Background(), fixtureConfig(address), mt4.Options{History: &mt4.HistoryRange{Start: 1700000000, End: 1700000015}})
			if err != nil {
				t.Fatal(err)
			}
			if err := <-completed; err != nil {
				t.Fatal(err)
			}
			if !result.HistoryComplete || len(result.History) != 1 || result.History[0].Comment != comment {
				t.Fatal("comment lost or read beyond its wire boundary")
			}
		})
	}
}
