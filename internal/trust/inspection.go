package trust

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/links"
)

var (
	ErrUnsafeInspectionTarget  = errors.New("unsafe inspection target")
	ErrUnsafeInspectionAddress = errors.New("unsafe inspection address")
	ErrInspectionResolution    = errors.New("inspection resolution failed")
)

// IPResolver is the DNS seam used by destination inspection. Production uses
// net.DefaultResolver; deterministic CI can inject a controlled resolver.
type IPResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

// ContextDialer is the outbound connection seam used after address validation.
type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

// DialContextFunc adapts a function to ContextDialer.
type DialContextFunc func(context.Context, string, string) (net.Conn, error)

func (f DialContextFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return f(ctx, network, address)
}

// InspectionTarget is the canonical, DNS-resolved target approved for one
// outbound inspection attempt. Addresses are pinned as textual IP literals so
// callers never need to resolve the hostname again through an unguarded path.
type InspectionTarget struct {
	CanonicalURL string   `json:"canonical_url"`
	Hostname     string   `json:"hostname"`
	Addresses    []string `json:"addresses"`
}

type defaultIPResolver struct{}

func (defaultIPResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

var metadataInspectionPrefixes = []netip.Prefix{
	netip.MustParsePrefix("169.254.169.254/32"),
	netip.MustParsePrefix("169.254.170.2/32"),
	netip.MustParsePrefix("100.100.100.200/32"),
	netip.MustParsePrefix("fd00:ec2::254/128"),
}

var reservedInspectionPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

// ValidateInspectionTarget preserves P05 canonical destination semantics, then
// adds P16's stricter inspection gate. Every DNS answer must be public-routable;
// one unsafe answer rejects the whole hostname to prevent mixed-answer bypass.
func ValidateInspectionTarget(ctx context.Context, raw string, resolver IPResolver) (InspectionTarget, error) {
	if containsUnsafeURLSyntax(raw) {
		return InspectionTarget{}, ErrUnsafeInspectionTarget
	}

	canonical, err := links.NormalizeDestination(raw)
	if err != nil {
		return InspectionTarget{}, fmt.Errorf("%w: %v", ErrUnsafeInspectionTarget, err)
	}
	u, err := url.Parse(canonical)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil {
		return InspectionTarget{}, ErrUnsafeInspectionTarget
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return InspectionTarget{}, ErrUnsafeInspectionTarget
	}
	if err := validateInspectionPort(u.Port()); err != nil {
		return InspectionTarget{}, err
	}

	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	addresses, err := resolveAndValidateInspectionHost(ctx, host, resolver)
	if err != nil {
		return InspectionTarget{}, err
	}
	return InspectionTarget{
		CanonicalURL: canonical,
		Hostname:     host,
		Addresses:    addresses,
	}, nil
}

// InspectionAddressClass returns the deny category for an address, or "public"
// when the address is eligible for outbound inspection.
func InspectionAddressClass(ip net.IP) string {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return "malformed"
	}
	addr = addr.Unmap()
	for _, prefix := range metadataInspectionPrefixes {
		if prefix.Contains(addr) {
			return "metadata-service"
		}
	}
	if addr.IsUnspecified() {
		return "unspecified"
	}
	if addr.IsLoopback() {
		return "loopback"
	}
	if addr.IsPrivate() {
		return "private"
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return "link-local"
	}
	if addr.IsMulticast() {
		return "multicast"
	}
	for _, prefix := range reservedInspectionPrefixes {
		if prefix.Contains(addr) {
			return "reserved"
		}
	}
	return "public"
}

// SafeInspectionDialer re-resolves the hostname at connection time, rejects any
// changed unsafe answer, and dials a validated IP literal. This closes the DNS
// rebinding gap between preflight validation and the actual socket connection.
type SafeInspectionDialer struct {
	Resolver IPResolver
	Dialer   ContextDialer
}

func (d SafeInspectionDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid dial authority", ErrUnsafeInspectionTarget)
	}
	if err := validateInspectionPort(port); err != nil {
		return nil, err
	}
	addresses, err := resolveAndValidateInspectionHost(ctx, strings.Trim(host, "[]"), d.Resolver)
	if err != nil {
		return nil, err
	}

	dialer := d.Dialer
	if dialer == nil {
		dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	}
	var lastErr error
	eligible := 0
	for _, rawIP := range addresses {
		addr, parseErr := netip.ParseAddr(rawIP)
		if parseErr != nil {
			continue
		}
		if network == "tcp4" && !addr.Is4() {
			continue
		}
		if network == "tcp6" && addr.Is4() {
			continue
		}
		eligible++
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if eligible == 0 {
		return nil, fmt.Errorf("%w: no address for network %s", ErrInspectionResolution, network)
	}
	if lastErr == nil {
		lastErr = ErrInspectionResolution
	}
	return nil, lastErr
}

// NewInspectionHTTPClient returns a proxy-free client whose initial request,
// redirects and socket dials all pass through the same P16 address policy.
func NewInspectionHTTPClient(resolver IPResolver, dialer ContextDialer) *http.Client {
	if resolver == nil {
		resolver = defaultIPResolver{}
	}
	safeDialer := SafeInspectionDialer{Resolver: resolver, Dialer: dialer}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           safeDialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          16,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	guarded := inspectionRoundTripper{base: transport, resolver: resolver}
	return &http.Client{
		Transport: guarded,
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("%w: redirect limit", ErrUnsafeInspectionTarget)
			}
			if req == nil || req.URL == nil {
				return ErrUnsafeInspectionTarget
			}
			_, err := ValidateInspectionTarget(req.Context(), req.URL.String(), resolver)
			return err
		},
	}
}

type inspectionRoundTripper struct {
	base     http.RoundTripper
	resolver IPResolver
}

func (t inspectionRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil {
		return nil, ErrUnsafeInspectionTarget
	}
	if _, err := ValidateInspectionTarget(req.Context(), req.URL.String(), t.resolver); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}

func resolveAndValidateInspectionHost(ctx context.Context, host string, resolver IPResolver) ([]string, error) {
	host = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(host, ".")))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return nil, fmt.Errorf("%w: loopback hostname", ErrUnsafeInspectionAddress)
	}

	if literal, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		literal = literal.Unmap()
		if class := InspectionAddressClass(net.IP(literal.AsSlice())); class != "public" {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeInspectionAddress, class)
		}
		return []string{literal.String()}, nil
	}
	if resolver == nil {
		resolver = defaultIPResolver{}
	}
	resolved, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInspectionResolution, err)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("%w: empty DNS answer", ErrInspectionResolution)
	}

	seen := make(map[string]struct{}, len(resolved))
	addresses := make([]string, 0, len(resolved))
	for _, item := range resolved {
		addr, ok := netip.AddrFromSlice(item.IP)
		if !ok {
			return nil, fmt.Errorf("%w: malformed DNS address", ErrUnsafeInspectionAddress)
		}
		addr = addr.Unmap()
		if class := InspectionAddressClass(net.IP(addr.AsSlice())); class != "public" {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeInspectionAddress, class)
		}
		value := addr.String()
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		addresses = append(addresses, value)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("%w: no usable DNS address", ErrInspectionResolution)
	}
	sort.Strings(addresses)
	return addresses, nil
}

func containsUnsafeURLSyntax(raw string) bool {
	if strings.Contains(raw, "\\") {
		return true
	}
	for _, r := range raw {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func validateInspectionPort(port string) error {
	if port == "" {
		return nil
	}
	value, err := strconv.Atoi(port)
	if err != nil || value < 1 || value > 65535 {
		return fmt.Errorf("%w: invalid port", ErrUnsafeInspectionTarget)
	}
	return nil
}
