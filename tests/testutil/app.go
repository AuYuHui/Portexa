// Package testutil supplies synthetic product fixtures. No fixture dials MT4.
package testutil

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"portexa/internal/accounts"
	"portexa/internal/auth"
	"portexa/internal/storage"
	"portexa/internal/vault"
	"portexa/mt4"
)

const Password = "synthetic-login-password-2026"
const InvestorPassword = "investor-demo"

type Fixture struct {
	Store    *storage.Store
	Accounts *accounts.Service
	Auth     *auth.Service
	Vault    *vault.Vault
}

func New(t testing.TB, login bool) Fixture {
	t.Helper()
	store, err := storage.Open(filepath.Join(t.TempDir(), "db", "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	key, err := vault.New([]byte(strings.Repeat("a", 32)))
	if err != nil {
		t.Fatal(err)
	}
	f := Fixture{Store: store, Vault: key, Accounts: accounts.New(store, key, func(ip string) bool { return ip == "203.0.113.9" || ip == "203.0.113.10" }), Auth: auth.New(store, auth.Options{})}
	if login {
		if err = f.Auth.Initialize(context.Background(), Password); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f Fixture) Server(t testing.TB, name string) (storage.Server, storage.Endpoint) {
	t.Helper()
	server, endpoint, err := f.Accounts.CreateServer(context.Background(), accounts.ServerInput{BrokerName: "Synthetic Broker", ServerName: name, IPAddress: "203.0.113.9", Port: 443, TimeZone: "Etc/UTC"})
	if err != nil {
		t.Fatal(err)
	}
	endpoint, err = f.Accounts.Review(context.Background(), endpoint.ID, accounts.ReviewInput{EvidenceRef: "synthetic-independent-evidence", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true})
	if err != nil {
		t.Fatal(err)
	}
	return server, endpoint
}

func (f Fixture) Account(t testing.TB, server storage.Server, endpoint storage.Endpoint, login uint32, currency string) storage.Account {
	t.Helper()
	account, err := f.Accounts.CreateAccount(context.Background(), accounts.AccountInput{ServerID: server.ID, EndpointID: endpoint.ID, Login: login, Label: "合成账户", Password: InvestorPassword, ConfirmedCurrency: currency, ConfirmUnit: true, SyncEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return account
}

func Result(currency string) mt4.Result {
	return mt4.Result{SessionConfirmed: true, AccountRead: true, Account: mt4.Account{Balance: 1000.01, Credit: 50, Equity: 1062.13, Currency: currency, Leverage: 100},
		Trades: []mt4.Trade{{Ticket: 10001, Symbol: "EURUSD", Command: 0, Digits: 5, VolumeHundredths: 12, OpenTime: 1700000000, OpenPrice: 1.1, ClosePrice: 1.1012, Profit: 12.34, Commission: -0.11, Swap: -0.11}}}
}
