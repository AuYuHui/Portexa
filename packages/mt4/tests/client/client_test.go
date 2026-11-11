package client_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"portexa/mt4"
	"portexa/mt4/tests/support"
)

func TestFetchThroughPublicInterface(t *testing.T) {
	address, completed := support.NewServer(t, support.Options{Authenticate: true, History: true})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := mt4.Fetch(ctx, mt4.Config{Address: address, Server: support.Server, Login: support.Login, Password: support.Password}, mt4.Options{History: &mt4.HistoryRange{Start: 1700000000, End: 1700003600}})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if !result.SessionConfirmed || !result.AccountRead || !result.HistoryRead || result.Account.Balance != 1234.5 || result.Account.Credit != 6.25 || result.Account.Leverage != 100 || result.Account.InvestorIndicator != 1 {
		t.Fatal("public snapshot lost account values or synchronization state")
	}
	if len(result.Trades) != 1 || len(result.History) != 1 || result.Trades[0].Command != 0 || result.History[0].Command != 1 || result.Trades[0].SL != 1.125 || result.History[0].Commission != -1.5 || result.TradeTrailingBytes != 0 {
		t.Fatal("public snapshot lost trade fields or record boundaries")
	}
}

func TestProbeClosesWithoutAuthentication(t *testing.T) {
	address, completed := support.NewServer(t, support.Options{})
	result, err := mt4.Probe(context.Background(), mt4.Config{Address: address, Login: support.Login, Password: support.Password})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-completed; err != nil {
		t.Fatal(err)
	}
	if !result.FirstResponseDecoded || result.SessionConfirmed || result.AccountRead || result.ServerBuild != 1470 {
		t.Fatal("initial probe was confused with full authentication")
	}
}

func TestContextCancellationInterruptsRead(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ready := make(chan struct{})
	peerDone := make(chan struct{})
	go func() {
		defer close(peerDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = io.CopyN(io.Discard, conn, 28)
		close(ready)
		_, _ = io.Copy(io.Discard, conn)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := mt4.Probe(ctx, mt4.Config{Address: listener.Addr().String(), Login: support.Login, Password: support.Password})
		done <- err
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("synthetic peer did not receive the initial request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("context cancellation did not interrupt the blocked socket")
	}
	<-peerDone
}

func TestInvalidHistoryRejectedBeforeDial(t *testing.T) {
	_, err := mt4.Fetch(context.Background(), mt4.Config{Address: "127.0.0.1:1", Server: support.Server, Login: support.Login, Password: support.Password}, mt4.Options{History: &mt4.HistoryRange{Start: 200, End: 100}})
	if err == nil || err.Error() != "history interval must be a valid increasing Unix time range" {
		t.Fatal("invalid history was not rejected before network setup")
	}
}
