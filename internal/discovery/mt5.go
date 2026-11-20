package discovery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	MT5SourceURL  = "https://mt5.mtapi.io/Search"
	MT5SourceName = "MTAPI MT5 Directory"
)

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

func (c *Catalog) parseMT5(ctx context.Context, body []byte, now time.Time) ([]Candidate, []ServerName, error) {
	var companies []struct {
		CompanyName string `json:"companyName"`
		Results     []struct {
			Name   string   `json:"name"`
			Access []string `json:"access"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &companies); err != nil || companies == nil {
		return nil, nil, ErrUnavailable
	}
	digest := sha256.Sum256(body)
	seen := make(map[string]bool)
	candidates := make([]Candidate, 0)
	names := make([]ServerName, 0)
	resolved := map[string][]Access{}
	lookups := 0

	for _, company := range companies {
		broker := strings.TrimSpace(company.CompanyName)
		if !validText(broker, 128) {
			continue
		}
		for _, server := range company.Results {
			name := strings.TrimSpace(server.Name)
			key := strings.ToLower(broker) + "\x00" + strings.ToLower(name)
			if !validText(name, 128) || seen[key] {
				continue
			}
			seen[key] = true
			names = append(names, ServerName{BrokerName: broker, ServerName: name})

			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return nil, nil, ErrUnavailable
			}
			candidate := Candidate{
				ID:             hex.EncodeToString(id[:]),
				Protocol:       "mt5",
				BrokerName:     broker,
				ServerName:     name,
				Demo:           strings.Contains(strings.ToLower(name), "demo"),
				Endpoints:      make([]Access, 0),
				SourceName:     MT5SourceName,
				SourceURL:      MT5SourceURL,
				FetchedAtMS:    now.UnixMilli(),
				ExpiresAtMS:    now.Add(15 * time.Minute).UnixMilli(),
				EvidenceSHA256: digest[:],
				EvidenceBody:   body,
			}

			for _, address := range server.Access {
				access, ok := resolved[address]
				if !ok {
					access = c.addresses(ctx, address, &lookups)
					resolved[address] = access
				}
				for _, endpoint := range access {
					duplicate := false
					for _, known := range candidate.Endpoints {
						if known == endpoint {
							duplicate = true
							break
						}
					}
					if !duplicate && len(candidate.Endpoints) < 24 {
						candidate.Endpoints = append(candidate.Endpoints, endpoint)
					}
				}
			}
			candidates = append(candidates, candidate)
			if len(candidates) >= 200 {
				break
			}
		}
		if len(candidates) >= 200 {
			break
		}
	}
	return candidates, names, nil
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
