package discovery_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"portexa/internal/discovery"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderUsesOnlyMT4DirectoryAndEncodesQuery(t *testing.T) {
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != discovery.SourceURL || r.Method != "POST" {
			t.Fatal("unexpected upstream", r.URL)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil || !strings.HasPrefix(string(raw), "company=Synthetic+%26+Company-Live+9&code=mt4&signature=") {
			t.Fatal("directory form order/escaping changed", err)
		}
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("company") != "Synthetic & Company-Live 9" || r.Form.Get("code") != "mt4" || r.Form.Get("ver") != "2" || len(r.Form.Get("signature")) != 32 {
			t.Fatal("invalid directory form", r.Form.Get("company"))
		}
		for _, field := range []string{"login", "password", "token", "key"} {
			if r.Form.Get(field) != "" {
				t.Fatal("credential field sent", field)
			}
		}
		if len(r.Form) != 4 || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("unexpected authentication material")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(document)), Request: r}, nil
	})}
	data, err := discovery.HTTPFetcher(client)(context.Background(), "Synthetic & Company-Live 9")
	if err != nil || string(data) != document {
		t.Fatal(err)
	}
}

func TestProviderRejectsRedirectsAndExcessiveBodies(t *testing.T) {
	for _, redirect := range []bool{true, false} {
		calls := 0
		client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
			calls++
			response := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(strings.Repeat("a", discovery.MaxBody+1))), Request: r}
			if redirect {
				response.StatusCode = 302
				response.Header.Set("Location", "http://127.0.0.1/private")
			}
			return response, nil
		})}
		if _, err := discovery.HTTPFetcher(client)(context.Background(), "Synthetic"); !errors.Is(err, discovery.ErrUnavailable) {
			t.Fatal("untrusted response accepted", err)
		}
		if calls != 1 {
			t.Fatal("redirect followed")
		}
	}
}

func TestMT5DirectorySendsOnlySearchTextAndReturnsNames(t *testing.T) {
	query := "Synthetic & Server"
	calls := 0
	client := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		calls++
		values := r.URL.Query()
		if r.Method != http.MethodGet || r.URL.Scheme+"://"+r.URL.Host+r.URL.Path != discovery.MT5SourceURL ||
			len(values) != 1 || values.Get("company") != query ||
			r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Fatal("unexpected MT5 directory request", r.Method, r.URL)
		}
		body := `[{"companyName":"Synthetic Company","results":[{"name":"Synthetic Demo"},{"name":"Synthetic & Server","access":["203.0.113.9:2085"]},{"name":"SYNTHETIC & SERVER"}]}]`
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
	catalog := discovery.New(discovery.Options{
		Fetch:       func(context.Context, string) ([]byte, error) { return []byte(`{"result":[]}`), nil },
		FetchMT5:    discovery.MT5HTTPFetcher(client),
		AllowTarget: func(string) bool { return true },
	})
	if results, err := catalog.Search(t.Context(), query); err != nil || len(results) != 0 {
		t.Fatalf("MT4 results: %+v (%v)", results, err)
	}
	for range 2 {
		results, err := catalog.SearchMT5(t.Context(), query)
		if err != nil || len(results) != 2 || results[0].ServerName != query || results[0].BrokerName != "Synthetic Company" {
			t.Fatalf("MT5 results: %+v (%v)", results, err)
		}
		if results[0].Protocol != "mt5" || len(results[0].Endpoints) != 1 || results[0].Endpoints[0].IPAddress != "203.0.113.9" || results[0].Endpoints[0].Port != 2085 {
			t.Fatalf("MT5 endpoints mismatch: %+v", results[0])
		}
	}
	if calls != 1 {
		t.Fatalf("MT5 directory was called %d times", calls)
	}
}
