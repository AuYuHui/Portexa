package client_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"portexa/mt4"
	"portexa/mt4/tests/support"
)

func fixtureConfig(address string) mt4.Config {
	return mt4.Config{Address: address, Server: support.Server, Login: support.Login, Password: support.Password, PasswordType: mt4.InvestorPassword}
}

func TestHistoryTimeoutRetainsFailingOperation(t *testing.T) {
	address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true, HistoryPages: []support.HistoryPage{{WaitForClose: true}}})
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := mt4.Fetch(ctx, fixtureConfig(address), mt4.Options{History: &mt4.HistoryRange{Start: 1700000000, End: 1700000015}})
	requireCode(t, err, mt4.Timeout, "history")
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
}

func requireCode(t *testing.T, err error, code mt4.ErrorCode, operation string) {
	t.Helper()
	var failure *mt4.Error
	if !errors.As(err, &failure) || failure.Code != code || failure.Operation != operation {
		t.Fatalf("expected %s at %s, got %v", code, operation, err)
	}
}

func TestHistoryOverlappingWindowsCorrectionsAndRepeatedSnapshots(t *testing.T) {
	first := append(support.TradeBytes(12, 1700000009, 2), support.TradeBytes(11, 1700000008, 1)...)
	second := append(support.TradeBytes(12, 1700000009, 3), support.TradeBytes(13, 1700000014, 4)...)
	second = append(second, support.TradeBytes(13, 1700000014, 4)...)
	var prior []mt4.Trade
	for attempt := 0; attempt < 2; attempt++ {
		address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true, HistoryPages: []support.HistoryPage{{Records: first}, {Records: second}}, HistoryBounds: [][2]uint32{{1700000000, 1700000010}, {1700000009, 1700000015}}})
		result, err := mt4.Fetch(context.Background(), fixtureConfig(address), mt4.Options{History: &mt4.HistoryRange{Start: 1700000000, End: 1700000015}, HistoryWindowSeconds: 10})
		if err != nil {
			t.Fatal(err)
		}
		if err := <-completed; err != nil {
			t.Fatal(err)
		}
		if !result.HistoryRead || !result.HistoryComplete || len(result.HistoryCovered) != 2 || len(result.History) != 3 || result.History[0].Ticket != 11 || result.History[1].Ticket != 12 || result.History[1].Profit != 3 || result.History[2].Ticket != 13 {
			t.Fatal("overlapping history lost, duplicated or failed to update a ticket")
		}
		if attempt == 1 && !reflect.DeepEqual(prior, result.History) {
			t.Fatal("repeated query changed an unchanged history snapshot")
		}
		prior = result.History
	}
}

func TestHistoryIncompleteWindowPreservesOnlyCommittedProgress(t *testing.T) {
	for _, test := range []struct {
		name string
		page support.HistoryPage
		code mt4.ErrorCode
	}{
		{"disconnect", support.HistoryPage{Disconnect: true}, mt4.ConnectionFailed},
		{"rejected", support.HistoryPage{Status: 5}, mt4.ServerRejected},
		{"limit", support.HistoryPage{DeclaredSize: 16<<20 + 1}, mt4.ResourceLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true, HistoryPages: []support.HistoryPage{{Records: support.TradeBytes(11, 1700000008, 1)}, test.page}})
			result, err := mt4.Fetch(context.Background(), fixtureConfig(address), mt4.Options{History: &mt4.HistoryRange{Start: 1700000000, End: 1700000015}, HistoryWindowSeconds: 10})
			requireCode(t, err, test.code, "history")
			if err := <-completed; err != nil {
				t.Fatal(err)
			}
			if result.HistoryRead || result.HistoryComplete || result.AccountRead || len(result.History) != 1 || len(result.HistoryCovered) != 1 {
				t.Fatal("failed history window was committed or complete progress was lost")
			}
		})
	}
}

func TestEmptyHistoryWindowsDoNotEraseOtherWindows(t *testing.T) {
	for _, pages := range [][]support.HistoryPage{
		{{Status: 1}, {Records: support.TradeBytes(11, 1700000014, 1)}},
		{{Records: support.TradeBytes(11, 1700000008, 1)}, {Status: 1}},
		{{Status: 1}, {Status: 1}},
	} {
		address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true, HistoryPages: pages})
		result, err := mt4.Fetch(context.Background(), fixtureConfig(address), mt4.Options{History: &mt4.HistoryRange{Start: 1700000000, End: 1700000015}, HistoryWindowSeconds: 10})
		if err != nil {
			t.Fatal(err)
		}
		if err := <-completed; err != nil {
			t.Fatal(err)
		}
		expected := 1
		if pages[0].Status == 1 && pages[1].Status == 1 {
			expected = 0
		}
		if !result.HistoryRead || !result.HistoryComplete || len(result.HistoryCovered) != 2 || len(result.History) != expected {
			t.Fatal("empty history window discarded another window or prevented completion")
		}
	}
}

func TestConflictingHistoryAndPartialAccountAreRejected(t *testing.T) {
	conflict := append(support.TradeBytes(11, 1700000008, 1), support.TradeBytes(11, 1700000008, 2)...)
	address, _ := support.NewServer(t, support.Options{Authenticate: true, History: true, HistoryPages: []support.HistoryPage{{Records: conflict}}})
	result, err := mt4.Fetch(context.Background(), fixtureConfig(address), mt4.Options{History: &mt4.HistoryRange{Start: 1700000000, End: 1700000015}})
	requireCode(t, err, mt4.InvalidResponse, "history")
	if result.HistoryRead || len(result.HistoryCovered) != 0 || len(result.History) != 0 {
		t.Fatal("conflicting page was partially committed")
	}
	address, completed := support.NewServer(t, support.Options{Authenticate: true, AccountTail: []byte{1}})
	result, err = mt4.Fetch(context.Background(), fixtureConfig(address), mt4.Options{})
	requireCode(t, err, mt4.InvalidResponse, "account")
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if result.AccountRead || result.TradeTrailingBytes != 1 {
		t.Fatal("truncated account orders were treated as a complete snapshot")
	}
}
