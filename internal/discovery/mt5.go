package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
)

const MT5SourceURL = "https://mt5.mtapi.io/Search"

type ServerName struct {
	BrokerName string `json:"broker_name"`
	ServerName string `json:"server_name"`
}

func MT5HTTPFetcher(client *http.Client) Fetcher {
	bounded := directoryClient(client)
	return func(ctx context.Context, query string) ([]byte, error) {
		params := url.Values{"company": {query}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, MT5SourceURL+"?"+params.Encode(), nil)
		if err != nil {
			return nil, ErrUnavailable
		}
		req.Header.Set("Accept", "text/json")
		return readDirectory(bounded, req)
	}
}

func parseMT5Names(body []byte, query string) ([]ServerName, error) {
	var companies []struct {
		CompanyName string `json:"companyName"`
		Results     []struct {
			Name string `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &companies); err != nil || companies == nil {
		return nil, ErrUnavailable
	}
	seen := make(map[ServerName]bool)
	names := make([]ServerName, 0)
	for _, company := range companies {
		broker := strings.TrimSpace(company.CompanyName)
		if !validText(broker, 128) {
			continue
		}
		for _, server := range company.Results {
			name := strings.TrimSpace(server.Name)
			key := ServerName{BrokerName: strings.ToLower(broker), ServerName: strings.ToLower(name)}
			if !validText(name, 128) || seen[key] {
				continue
			}
			seen[key] = true
			names = append(names, ServerName{BrokerName: broker, ServerName: name})
		}
	}
	sort.SliceStable(names, func(i, j int) bool {
		return strings.EqualFold(names[i].ServerName, query) && !strings.EqualFold(names[j].ServerName, query)
	})
	if len(names) > 200 {
		names = slices.Clone(names[:200])
	}
	return names, nil
}
