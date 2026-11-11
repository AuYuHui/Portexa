// Synthetic browser fixture. This executable never calls the production SDK.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"portexa/internal/accounts"
	"portexa/internal/auth"
	"portexa/internal/discovery"
	"portexa/internal/httpapi"
	"portexa/internal/storage"
	"portexa/internal/syncer"
	"portexa/internal/vault"
	"portexa/mt4"
	"portexa/tests/testutil"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	dir, err := os.MkdirTemp("", "portexa-e2e-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	store, err := storage.Open(filepath.Join(dir, "db", "portexa.sqlite"))
	if err != nil {
		return err
	}
	defer store.Close()
	key, err := vault.New([]byte(strings.Repeat("e", 32)))
	if err != nil {
		return err
	}
	service := accounts.New(store, key, func(ip string) bool { return ip == "203.0.113.9" || ip == "203.0.113.10" })
	authService := auth.New(store, auth.Options{})
	if err = authService.Initialize(ctx, testutil.Password); err != nil {
		return err
	}
	fetch := func(ctx context.Context, config mt4.Config, options mt4.Options) (mt4.Result, error) {
		select {
		case <-time.After(120 * time.Millisecond):
		case <-ctx.Done():
			return mt4.Result{}, ctx.Err()
		}
		var account storage.Account
		if err := store.Read.WithContext(ctx).First(&account, "login = ?", config.Login).Error; err != nil {
			return mt4.Result{}, err
		}
		currency := "USD"
		if account.Currency != nil {
			currency = *account.Currency
		}
		return testutil.Result(currency), nil
	}
	server, endpoint, err := service.CreateServer(ctx, accounts.ServerInput{BrokerName: "Synthetic Markets", ServerName: "Synthetic-Live01", IPAddress: "203.0.113.9", Port: 443, TimeZone: "Europe/Helsinki"})
	if err != nil {
		return err
	}
	endpoint, err = service.Review(ctx, endpoint.ID, accounts.ReviewInput{EvidenceRef: "synthetic-fixture", EvidenceSHA256: strings.Repeat("ab", 32), AcceptRisk: true})
	if err != nil {
		return err
	}
	for i, label := range []string{"稳健策略 · 美元", "美分账户 · 多策略", "欧元观察账户", "备用账户 · 已暂停", "新接入 · 等待采集"} {
		currency := []string{"USD", "USC", "EUR", "USD", "USD"}[i]
		account, err := service.CreateAccount(ctx, accounts.AccountInput{ServerID: server.ID, EndpointID: endpoint.ID, Login: uint32(100101 + i), Label: label, Password: testutil.InvestorPassword, ConfirmedCurrency: currency, ConfirmUnit: true, SyncEnabled: true})
		if err != nil {
			return err
		}
		if i < 4 {
			job, err := service.Claim(ctx, account.ID)
			if err != nil {
				return err
			}
			result := testutil.Result(currency)
			result.Account.Balance = []float64{12450.5, 824500, 3500, 6200}[i]
			result.Account.Credit = 0
			result.Trades[0].Profit = []float64{122.32, 3150.75, 55.42, 0.22}[i]
			result.Account.Equity = result.Account.Balance + result.Trades[0].NetProfit()
			stamp := time.Now().UnixMilli() - int64(i*1300)
			if i == 3 {
				stamp -= 5 * 60 * 1000
			}
			snapshot, positions, err := syncer.Convert(job, result, stamp-450, stamp)
			if err != nil {
				return err
			}
			if err = service.Publish(ctx, job, snapshot, positions, time.Now().Add(time.Hour).UnixMilli()); err != nil {
				return err
			}
		}
		if i >= 3 {
			enabled := false
			if err = service.UpdateAccount(ctx, account.ID, accounts.AccountPatch{SyncEnabled: &enabled}); err != nil {
				return err
			}
		}
	}
	engine := syncer.New(service, fetch, syncer.Options{Interval: time.Hour})
	directory := discovery.New(discovery.Options{AllowTarget: service.AllowTarget, Fetch: func(ctx context.Context, query string) ([]byte, error) {
		result := []map[string]any{}
		if strings.Contains(strings.ToLower(query), "directory") {
			result = append(result, map[string]any{"company": "Synthetic Directory Broker", "results": []map[string]any{{"name": "Synthetic-DirectoryLive", "access": []string{"203.0.113.9:443", "203.0.113.10:443"}, "is_demo": false}}})
		}
		return json.Marshal(map[string]any{"result": result})
	}})
	handler, err := httpapi.New(authService, service, engine, httpapi.Config{Origin: "http://127.0.0.1:18080", DevelopmentHTTP: true, Static: os.DirFS(".local/web"), Directory: directory})
	if err != nil {
		return err
	}
	httpServer := &http.Server{Addr: "127.0.0.1:18080", Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- engine.Run(ctx) }()
	go func() { <-ctx.Done(); _ = httpServer.Close() }()
	fmt.Println("Synthetic browser fixture on 127.0.0.1:18080")
	err = httpServer.ListenAndServe()
	cancel()
	engineErr := <-done
	if err == http.ErrServerClosed {
		err = nil
	}
	if err != nil {
		return err
	}
	return engineErr
}
