package discovery_test

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"

	"portexa/internal/discovery"
)

const document = `{"result":[{"company":"Synthetic Markets","results":[{"name":"Synthetic-Live 9","access":["203.0.113.9:443","203.0.113.9:443","127.0.0.1:443","primary.synthetic.example:8443","missing-port.synthetic.example"],"is_demo":0},{"name":"Synthetic-Demo","access":[],"is_demo":true}]}]}`

func allow(ip string) bool { return strings.HasPrefix(ip, "203.0.113.") }

func TestSearchCandidatesCacheExpirationAndDNSFiltering(t *testing.T) {
	now := time.Now()
	calls, dnsCalls := 0, 0
	catalog := discovery.New(discovery.Options{Now: func() time.Time { return now }, AllowTarget: allow,
		Fetch: func(ctx context.Context, q string) ([]byte, error) { calls++; return []byte(document), nil },
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			dnsCalls++
			if host != "primary.synthetic.example" {
				t.Fatal("unexpected DNS query", host)
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("203.0.113.10")}, nil
		},
	})
	list, err := catalog.Search(context.Background(), "Synthetic-Live 9")
	if err != nil || len(list) != 2 {
		t.Fatal("search", err, len(list))
	}
	if list[0].ServerName != "Synthetic-Live 9" || len(list[0].Endpoints) != 2 || list[0].Endpoints[1].Port != 8443 {
		t.Fatal("filtering/deduplication failed", list[0].Endpoints)
	}
	if calls != 1 || dnsCalls != 1 {
		t.Fatal(calls, dnsCalls)
	}
	id := list[0].ID
	list[0].Endpoints[0].IPAddress = "127.0.0.1"
	stored, err := catalog.Candidate(id)
	if err != nil || stored.Endpoints[0].IPAddress != "203.0.113.9" {
		t.Fatal("caller mutated cached address", err)
	}
	if _, err = catalog.Search(context.Background(), "synthetic-live 9"); err != nil || calls != 1 {
		t.Fatal("cache miss", calls, err)
	}
	now = now.Add(16 * time.Minute)
	if _, err = catalog.Candidate(id); !errors.Is(err, discovery.ErrExpired) {
		t.Fatal("expired candidate accepted", err)
	}
	if _, err = catalog.Search(context.Background(), "Synthetic-Live 9"); err != nil || calls != 2 {
		t.Fatal("cache not refreshed", calls, err)
	}
}

func TestInvalidAndOversizedDirectoriesFailClosed(t *testing.T) {
	for _, body := range []string{"{}", `{"result":null}`, "<html>no directory</html>", strings.Repeat("x", discovery.MaxBody+1)} {
		catalog := discovery.New(discovery.Options{AllowTarget: allow, Fetch: func(context.Context, string) ([]byte, error) { return []byte(body), nil }})
		if _, err := catalog.Search(context.Background(), "Synthetic"); err == nil {
			t.Fatal("invalid directory accepted")
		}
	}
	catalog := discovery.New(discovery.Options{Fetch: func(context.Context, string) ([]byte, error) {
		t.Fatal("invalid query reached network")
		return nil, nil
	}})
	for _, q := range []string{"a", "a\ncompany", strings.Repeat("a", 81)} {
		if _, err := catalog.Search(context.Background(), q); !errors.Is(err, discovery.ErrQuery) {
			t.Fatal("invalid query accepted", q)
		}
	}
}

func TestNativeDirectoryPreludeIsAccepted(t *testing.T) {
	catalog := discovery.New(discovery.Options{AllowTarget: allow, Resolve: func(context.Context, string) ([]netip.Addr, error) { return nil, nil }, Fetch: func(context.Context, string) ([]byte, error) { return []byte(strings.Repeat("a", 44) + document), nil }})
	list, err := catalog.Search(context.Background(), "Synthetic-Live 9")
	if err != nil || len(list) != 2 || len(list[0].Endpoints) == 0 {
		t.Fatal("framed native directory was rejected", err)
	}
}

func TestDirectoryConcurrencyAndCancellation(t *testing.T) {
	entered := make(chan struct{}, 2)
	catalog := discovery.New(discovery.Options{AllowTarget: allow, Fetch: func(ctx context.Context, _ string) ([]byte, error) {
		entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 2)
	for _, q := range []string{"Synthetic A", "Synthetic B"} {
		go func() { _, err := catalog.Search(ctx, q); done <- err }()
	}
	<-entered
	<-entered
	if _, err := catalog.Search(ctx, "Synthetic C"); !errors.Is(err, discovery.ErrBusy) {
		t.Fatal("unbounded directory concurrency", err)
	}
	cancel()
	for i := 0; i < 2; i++ {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	}
}
