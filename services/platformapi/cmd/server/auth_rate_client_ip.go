package main

import (
	"net"
	"net/http"
	"net/netip"
	"strings"

	authn "github.com/Techshrr/GoJet/internal/auth"
)

const maxAuthForwardedHops = 32

type authTrustedProxies struct {
	prefixes []netip.Prefix
}

func parseAuthTrustedProxyCIDRs(raw string) (authTrustedProxies, error) {
	trusted := authTrustedProxies{
		prefixes: []netip.Prefix{
			netip.MustParsePrefix("127.0.0.0/8"),
			netip.MustParsePrefix("::1/128"),
		},
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "none") {
		return trusted, nil
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 64 {
		return authTrustedProxies{}, authn.ErrInvalid
	}
	seen := map[string]struct{}{
		"127.0.0.0/8": {},
		"::1/128":     {},
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return authTrustedProxies{}, authn.ErrInvalid
		}
		prefix, err := netip.ParsePrefix(part)
		if err != nil || !prefix.IsValid() || prefix.Addr().Is4In6() {
			return authTrustedProxies{}, authn.ErrInvalid
		}
		prefix = prefix.Masked()
		key := prefix.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		trusted.prefixes = append(trusted.prefixes, prefix)
	}
	return trusted, nil
}

func (t authTrustedProxies) contains(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range t.prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func authRateClientAddress(r *http.Request, trusted authTrustedProxies) string {
	if r == nil {
		return "unknown"
	}
	peer, ok := parseAuthPeerAddress(r.RemoteAddr)
	if !ok {
		return strings.TrimSpace(r.RemoteAddr)
	}
	if !trusted.contains(peer) {
		return peer.String()
	}

	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded == "" {
		return peer.String()
	}
	parts := strings.Split(forwarded, ",")
	if len(parts) == 0 || len(parts) > maxAuthForwardedHops {
		return peer.String()
	}

	current := peer
	for i := len(parts) - 1; i >= 0; i-- {
		if !trusted.contains(current) {
			break
		}
		hop, valid := parseAuthForwardedAddress(parts[i])
		if !valid {
			return peer.String()
		}
		current = hop
	}
	return current.String()
}

func parseAuthPeerAddress(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(strings.Trim(raw, "[]")); err == nil {
		return addr.Unmap(), true
	}
	host, _, err := net.SplitHostPort(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func parseAuthForwardedAddress(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "unknown") {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(strings.Trim(raw, "[]")); err == nil {
		return addr.Unmap(), true
	}
	host, _, err := net.SplitHostPort(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}
