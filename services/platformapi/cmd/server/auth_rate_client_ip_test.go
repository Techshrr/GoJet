package main

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

func TestParseAuthTrustedProxyCIDRs(t *testing.T) {
	t.Parallel()

	base, err := parseAuthTrustedProxyCIDRs("")
	if err != nil {
		t.Fatal(err)
	}
	if !base.contains(mustAuthTestAddr(t, "127.0.0.1")) || !base.contains(mustAuthTestAddr(t, "::1")) {
		t.Fatal("loopback proxy boundary must always be trusted")
	}
	if base.contains(mustAuthTestAddr(t, "10.1.2.3")) {
		t.Fatal("non-loopback proxy unexpectedly trusted without explicit configuration")
	}

	extra, err := parseAuthTrustedProxyCIDRs("10.0.0.0/8, 2001:db8:1::/48")
	if err != nil {
		t.Fatal(err)
	}
	if !extra.contains(mustAuthTestAddr(t, "10.1.2.3")) || !extra.contains(mustAuthTestAddr(t, "2001:db8:1::8")) {
		t.Fatal("explicit trusted proxy prefixes were not accepted")
	}

	for _, raw := range []string{"10.0.0.0/8,", "not-a-prefix", "192.0.2.1"} {
		if _, err := parseAuthTrustedProxyCIDRs(raw); err == nil {
			t.Fatalf("parseAuthTrustedProxyCIDRs(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestAuthRateClientAddressIgnoresForwardingFromUntrustedPeer(t *testing.T) {
	t.Parallel()
	trusted, err := parseAuthTrustedProxyCIDRs("")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"user@example.test"}`))
	req.RemoteAddr = "198.51.100.24:44321"
	req.Header.Set("X-Forwarded-For", "203.0.113.99")
	if got := authRateClientAddress(req, trusted); got != "198.51.100.24" {
		t.Fatalf("untrusted peer forwarding header was accepted: got %q", got)
	}
}

func TestAuthRateClientAddressUsesOriginBehindTrustedLoopbackProxy(t *testing.T) {
	t.Parallel()
	trusted, err := parseAuthTrustedProxyCIDRs("")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:4185"
	req.Header.Set("X-Forwarded-For", "198.51.100.24")
	if got := authRateClientAddress(req, trusted); got != "198.51.100.24" {
		t.Fatalf("trusted loopback proxy did not resolve origin: got %q", got)
	}
}

func TestAuthRateClientAddressWalksTrustedProxyChainRightToLeft(t *testing.T) {
	t.Parallel()
	trusted, err := parseAuthTrustedProxyCIDRs("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:4185"
	req.Header.Set("X-Forwarded-For", "192.0.2.200, 198.51.100.24, 10.10.0.8")
	if got := authRateClientAddress(req, trusted); got != "198.51.100.24" {
		t.Fatalf("trusted chain selected spoofable left entry: got %q", got)
	}
}

func TestAuthRateClientAddressIgnoresOversizedSpoofPrefixAfterOrigin(t *testing.T) {
	t.Parallel()
	trusted, err := parseAuthTrustedProxyCIDRs("")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:4185"
	req.Header.Set("X-Forwarded-For", strings.Repeat("192.0.2.200,", maxAuthForwardedHops+16)+"198.51.100.24")
	if got := authRateClientAddress(req, trusted); got != "198.51.100.24" {
		t.Fatalf("oversized attacker-controlled left prefix collapsed to proxy bucket: got %q", got)
	}
}

func TestAuthRateClientAddressBoundsAllTrustedChain(t *testing.T) {
	t.Parallel()
	trusted, err := parseAuthTrustedProxyCIDRs("10.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	parts := make([]string, maxAuthForwardedHops+1)
	for i := range parts {
		parts[i] = "10.0.0.8"
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:4185"
	req.Header.Set("X-Forwarded-For", strings.Join(parts, ","))
	if got := authRateClientAddress(req, trusted); got != "127.0.0.1" {
		t.Fatalf("unbounded all-trusted chain should fail safely to immediate peer: got %q", got)
	}
}

func TestAuthRateClientAddressFailsSafeOnMalformedTrustedChain(t *testing.T) {
	t.Parallel()
	trusted, err := parseAuthTrustedProxyCIDRs("")
	if err != nil {
		t.Fatal(err)
	}
	for _, forwarded := range []string{"unknown", "not-an-ip", "198.51.100.24, unknown"} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
		req.RemoteAddr = "127.0.0.1:4185"
		req.Header.Set("X-Forwarded-For", forwarded)
		if got := authRateClientAddress(req, trusted); got != "127.0.0.1" {
			t.Fatalf("malformed forwarded chain %q did not fall back to immediate trusted peer: got %q", forwarded, got)
		}
	}
}

func TestAuthRateClientAddressFallsBackWhenTrustedProxyHasNoForwardedHeader(t *testing.T) {
	t.Parallel()
	trusted, err := parseAuthTrustedProxyCIDRs("")
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:4185"
	if got := authRateClientAddress(req, trusted); got != "127.0.0.1" {
		t.Fatalf("missing forwarded header should fall back to immediate peer: got %q", got)
	}
}

func mustAuthTestAddr(t *testing.T, raw string) netip.Addr {
	t.Helper()
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		t.Fatal(err)
	}
	return addr
}
