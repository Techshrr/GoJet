package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Techshrr/GoJet/internal/trust"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

type scriptedResolver struct {
	mu      sync.Mutex
	scripts map[string][][]net.IPAddr
	calls   map[string]int
}

func newScriptedResolver() *scriptedResolver {
	return &scriptedResolver{scripts: map[string][][]net.IPAddr{}, calls: map[string]int{}}
}

func (r *scriptedResolver) set(host string, answers ...[]string) {
	sequence := make([][]net.IPAddr, 0, len(answers))
	for _, answer := range answers {
		ips := make([]net.IPAddr, 0, len(answer))
		for _, raw := range answer {
			ips = append(ips, net.IPAddr{IP: net.ParseIP(raw)})
		}
		sequence = append(sequence, ips)
	}
	r.scripts[strings.ToLower(host)] = sequence
}

func (r *scriptedResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	host = strings.ToLower(host)
	sequence := r.scripts[host]
	if len(sequence) == 0 {
		return nil, fmt.Errorf("fixture DNS has no answer for %s", host)
	}
	index := r.calls[host]
	r.calls[host] = index + 1
	if index >= len(sequence) {
		index = len(sequence) - 1
	}
	answer := sequence[index]
	out := make([]net.IPAddr, len(answer))
	copy(out, answer)
	return out, nil
}

func (r *scriptedResolver) callCount(host string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[strings.ToLower(host)]
}

type countingDialer struct {
	mu     sync.Mutex
	calls  int
	target string
}

func (d *countingDialer) DialContext(ctx context.Context, network, _ string) (net.Conn, error) {
	d.mu.Lock()
	d.calls++
	target := d.target
	d.mu.Unlock()
	if target == "" {
		return nil, errors.New("fixture dial must not be reached")
	}
	return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, network, target)
}

func (d *countingDialer) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T004"
		result.Status = "FAIL"
		if result.Checks == nil {
			result.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil || result.Status != "PASS" {
		os.Exit(1)
	}
}

func run() (output, error) {
	out := output{
		Case:         "P16-T004",
		Status:       "FAIL",
		Fixture:      "controlled in-process DNS scripts plus local HTTP redirect fixture; no external/private network access",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resolver := newScriptedResolver()
	unsafeHosts := map[string]string{
		"loopback.test":      "127.0.0.1",
		"private.test":       "10.10.0.7",
		"linklocal.test":     "169.254.1.9",
		"metadata.test":      "100.100.100.200",
		"reserved.test":      "192.0.2.10",
		"unspecified.test":   "0.0.0.0",
		"multicast.test":     "224.0.0.1",
		"v6-loopback.test":   "::1",
		"v6-private.test":    "fd00::7",
		"v6-linklocal.test":  "fe80::1",
		"v6-reserved.test":   "2001:db8::1",
		"metadata-link.test": "169.254.169.254",
	}
	for host, ip := range unsafeHosts {
		resolver.set(host, []string{ip})
	}

	unsafeRejected := true
	unsafeErrorAuthority := true
	for host := range unsafeHosts {
		_, err := trust.ValidateInspectionTarget(ctx, "http://"+host+"/", resolver)
		if err == nil {
			unsafeRejected = false
			continue
		}
		if !errors.Is(err, trust.ErrUnsafeInspectionAddress) {
			unsafeErrorAuthority = false
		}
	}

	resolver.set("mixed.test", []string{"8.8.8.8", "10.0.0.9"})
	_, mixedErr := trust.ValidateInspectionTarget(ctx, "https://mixed.test/", resolver)

	literalCallsBefore := resolver.callCount("private.test")
	_, literalErr := trust.ValidateInspectionTarget(ctx, "http://127.0.0.1/", resolver)
	literalCallsAfter := resolver.callCount("private.test")

	rebindingResolver := newScriptedResolver()
	rebindingResolver.set("rebinding.test", []string{"8.8.8.8"}, []string{"10.0.0.44"})
	rebindingDialer := &countingDialer{}
	rebindingClient := trust.NewInspectionHTTPClient(rebindingResolver, rebindingDialer)
	rebindingReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://rebinding.test:8080/", nil)
	if err != nil {
		return out, err
	}
	_, rebindingErr := rebindingClient.Do(rebindingReq)

	redirectResolver := newScriptedResolver()
	redirectResolver.set("safe-origin.test", []string{"8.8.8.8"}, []string{"8.8.8.8"})
	redirectResolver.set("redirect-private.test", []string{"169.254.169.254"})

	var redirectPort string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "http://redirect-private.test:"+redirectPort+"/secret", http.StatusFound)
	}))
	defer server.Close()
	_, redirectPort, err = net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		return out, err
	}
	redirectDialer := &countingDialer{target: server.Listener.Addr().String()}
	redirectClient := trust.NewInspectionHTTPClient(redirectResolver, redirectDialer)
	redirectReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://safe-origin.test:"+redirectPort+"/", nil)
	if err != nil {
		return out, err
	}
	_, redirectErr := redirectClient.Do(redirectReq)

	out.RecordCounts = map[string]int{
		"unsafe_address_classes":     len(unsafeHosts),
		"mixed_dns_answers":          2,
		"rebinding_dns_calls":        rebindingResolver.callCount("rebinding.test"),
		"rebinding_underlying_dials": rebindingDialer.callCount(),
		"redirect_origin_dns_calls":  redirectResolver.callCount("safe-origin.test"),
		"redirect_private_dns_calls": redirectResolver.callCount("redirect-private.test"),
		"redirect_underlying_dials":  redirectDialer.callCount(),
	}
	out.Checks = map[string]bool{
		"loopback_private_linklocal_reserved_metadata_rejected": unsafeRejected,
		"unsafe_addresses_use_address_error_authority":          unsafeErrorAuthority,
		"mixed_public_private_dns_answer_fails_closed":          errors.Is(mixedErr, trust.ErrUnsafeInspectionAddress),
		"literal_private_address_skips_dns_and_fails_closed":    errors.Is(literalErr, trust.ErrUnsafeInspectionAddress) && literalCallsBefore == literalCallsAfter,
		"dns_rebinding_revalidated_at_socket_dial":              errors.Is(rebindingErr, trust.ErrUnsafeInspectionAddress) && rebindingResolver.callCount("rebinding.test") >= 2,
		"dns_rebinding_never_reaches_underlying_dial":           rebindingDialer.callCount() == 0,
		"redirect_to_private_is_rejected_before_follow":         errors.Is(redirectErr, trust.ErrUnsafeInspectionAddress),
		"redirect_private_target_never_gets_socket_dial":        redirectDialer.callCount() == 1,
		"safe_origin_uses_guarded_pinned_ip_dial":               redirectResolver.callCount("safe-origin.test") >= 2 && redirectDialer.callCount() == 1,
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func allTrue(checks map[string]bool) bool {
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return len(checks) > 0
}
