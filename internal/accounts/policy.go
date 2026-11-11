package accounts

import (
	"net/netip"
	"strings"
)

type TargetPolicy func(string) bool

var excludedNetworks = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// PublicTarget is the production policy. Tests inject their own policy at
// construction; no HTTP field or development flag can disable this check.
func PublicTarget(value string) bool {
	address, err := netip.ParseAddr(value)
	if err != nil || address.Zone() != "" {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range excludedNetworks {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

func clean(value string, min, max int) bool {
	return len(strings.TrimSpace(value)) >= min && len(value) <= max && !strings.ContainsAny(value, "\x00\r\n")
}
