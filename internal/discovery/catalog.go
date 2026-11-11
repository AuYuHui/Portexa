package discovery

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

type Access struct {
	IPAddress string `json:"ip_address"`
	Port      int    `json:"port"`
}

type Candidate struct {
	ID             string   `json:"id"`
	BrokerName     string   `json:"broker_name"`
	ServerName     string   `json:"server_name"`
	Demo           bool     `json:"demo"`
	Endpoints      []Access `json:"endpoints"`
	SourceName     string   `json:"source_name"`
	SourceURL      string   `json:"source_url"`
	FetchedAtMS    int64    `json:"fetched_at_ms"`
	ExpiresAtMS    int64    `json:"expires_at_ms"`
	EvidenceSHA256 []byte   `json:"-"`
	EvidenceBody   []byte   `json:"-"`
}

type Options struct {
	Fetch       Fetcher
	AllowTarget func(string) bool
	Resolve     func(context.Context, string) ([]netip.Addr, error)
	Now         func() time.Time
}

type entry struct {
	candidates []Candidate
	expires    int64
	bytes      int
	created    int64
}
type Catalog struct {
	fetch   Fetcher
	allow   func(string) bool
	resolve func(context.Context, string) ([]netip.Addr, error)
	now     func() time.Time
	mu      sync.Mutex
	cache   map[string]entry
	slots   chan struct{}
}

func New(options Options) *Catalog {
	if options.Fetch == nil {
		options.Fetch = HTTPFetcher(nil)
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Resolve == nil {
		options.Resolve = func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
	}
	return &Catalog{fetch: options.Fetch, allow: options.AllowTarget, resolve: options.Resolve, now: options.Now, cache: map[string]entry{}, slots: make(chan struct{}, 2)}
}

func validText(text string, max int) bool {
	if !utf8.ValidString(text) || len(text) == 0 || len(text) > max {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func (c *Catalog) Search(ctx context.Context, query string) ([]Candidate, error) {
	query = strings.TrimSpace(query)
	if utf8.RuneCountInString(query) < 2 || utf8.RuneCountInString(query) > 80 || !validText(query, 320) {
		return nil, ErrQuery
	}
	key := strings.ToLower(query)
	c.mu.Lock()
	c.expireLocked()
	if cached, ok := c.cache[key]; ok {
		list := copyList(cached.candidates)
		c.mu.Unlock()
		return list, nil
	}
	c.mu.Unlock()
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrBusy
	}
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	body, err := c.fetch(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(body) > MaxBody {
		return nil, ErrUnavailable
	}
	now := c.now()
	list, err := c.parse(ctx, body, now)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool {
		iExact, jExact := strings.EqualFold(list[i].ServerName, query), strings.EqualFold(list[j].ServerName, query)
		if iExact != jExact {
			return iExact
		}
		if list[i].BrokerName != list[j].BrokerName {
			return list[i].BrokerName < list[j].BrokerName
		}
		return list[i].ServerName < list[j].ServerName
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()
	c.cache[key] = entry{candidates: list, expires: now.Add(15 * time.Minute).UnixMilli(), bytes: len(body), created: now.UnixNano()}
	for len(c.cache) > 32 || c.cacheBytes() > 8<<20 {
		oldest, at := "", int64(1<<63-1)
		for name, value := range c.cache {
			if value.created < at {
				oldest, at = name, value.created
			}
		}
		delete(c.cache, oldest)
	}
	return copyList(list), nil
}

func (c *Catalog) Candidate(id string) (Candidate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked()
	for _, cached := range c.cache {
		for _, candidate := range cached.candidates {
			if candidate.ID == id {
				return copyList([]Candidate{candidate})[0], nil
			}
		}
	}
	return Candidate{}, ErrExpired
}

func (c *Catalog) expireLocked() {
	for key, value := range c.cache {
		if value.expires <= c.now().UnixMilli() {
			delete(c.cache, key)
		}
	}
}
func (c *Catalog) cacheBytes() int {
	n := 0
	for _, value := range c.cache {
		n += value.bytes
	}
	return n
}
func copyList(list []Candidate) []Candidate {
	out := make([]Candidate, len(list))
	copy(out, list)
	for i := range out {
		out[i].Endpoints = append([]Access{}, out[i].Endpoints...)
		out[i].EvidenceSHA256 = append([]byte{}, out[i].EvidenceSHA256...)
	}
	return out
}

func (c *Catalog) parse(ctx context.Context, body []byte, now time.Time) ([]Candidate, error) {
	start := strings.IndexByte(string(body), '{')
	// The native directory returns a prelude before JSON (44 bytes observed).
	// Keep it bounded without rejecting legitimate framed directory responses.
	if start < 0 || start > 1024 {
		return nil, ErrUnavailable
	}
	var document struct {
		Result *[]struct {
			Company string `json:"company"`
			Results []struct {
				Name   string          `json:"name"`
				Access []string        `json:"access"`
				Demo   json.RawMessage `json:"is_demo"`
			} `json:"results"`
		} `json:"result"`
	}
	if json.Unmarshal(body[start:], &document) != nil || document.Result == nil {
		return nil, ErrUnavailable
	}
	digest := sha256.Sum256(body)
	result := make([]Candidate, 0)
	resolved := map[string][]Access{}
	seen := map[string]bool{}
	lookups := 0
	for _, company := range *document.Result {
		if !validText(company.Company, 128) {
			continue
		}
		for _, server := range company.Results {
			if !validText(server.Name, 128) || seen[company.Company+"\x00"+server.Name] {
				continue
			}
			seen[company.Company+"\x00"+server.Name] = true
			var id [16]byte
			if _, err := rand.Read(id[:]); err != nil {
				return nil, ErrUnavailable
			}
			candidate := Candidate{ID: hex.EncodeToString(id[:]), BrokerName: company.Company, ServerName: server.Name, Demo: string(server.Demo) == "1" || string(server.Demo) == "true", Endpoints: make([]Access, 0), SourceName: SourceName, SourceURL: SourceURL, FetchedAtMS: now.UnixMilli(), ExpiresAtMS: now.Add(15 * time.Minute).UnixMilli(), EvidenceSHA256: digest[:], EvidenceBody: body}
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
			result = append(result, candidate)
			if len(result) >= 200 {
				return result, nil
			}
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return result, nil
}

func (c *Catalog) addresses(ctx context.Context, address string, lookups *int) []Access {
	if c.allow == nil {
		return nil
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	} // Never guess a missing port.
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return nil
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if c.allow(ip.String()) {
			return []Access{{IPAddress: ip.String(), Port: port}}
		}
		return nil
	}
	if *lookups >= 16 || !validText(host, 253) || strings.ContainsAny(host, "/@:% ") || !strings.Contains(host, ".") {
		return nil
	}
	*lookups++
	dnsCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	addresses, err := c.resolve(dnsCtx, host)
	if err != nil {
		return nil
	}
	result := make([]Access, 0)
	for _, ip := range addresses {
		ip = ip.Unmap()
		if c.allow(ip.String()) && len(result) < 8 {
			result = append(result, Access{IPAddress: ip.String(), Port: port})
		}
	}
	return result
}
