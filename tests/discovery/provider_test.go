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
