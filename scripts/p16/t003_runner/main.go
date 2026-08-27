package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
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

type countingResolver struct {
	calls int
}

func (r *countingResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	r.calls++
	return []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, nil
}

func main() {
	result, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		result.Case = "P16-T003"
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
		Case:         "P16-T003",
		Status:       "FAIL",
		Fixture:      "deterministic in-process DNS resolver; no external provider access",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resolver := &countingResolver{}
	providerCalls := 0

	gate := func(raw string) (trust.InspectionTarget, error) {
		target, err := trust.ValidateInspectionTarget(ctx, raw, resolver)
		if err != nil {
			return trust.InspectionTarget{}, err
		}
		providerCalls++
		return target, nil
	}

	valid := []struct {
		raw       string
		canonical string
	}{
		{" HTTPS://Example.COM:443/a/../b?z=2&a=1#fragment ", "https://example.com/b?a=1&z=2"},
		{"http://Example.COM:80", "http://example.com/"},
		{"https://example.com:8443/path", "https://example.com:8443/path"},
	}
	validCanonical := true
	validResolved := true
	for _, tc := range valid {
		target, err := gate(tc.raw)
		if err != nil {
			validCanonical = false
			validResolved = false
			continue
		}
		if target.CanonicalURL != tc.canonical {
			validCanonical = false
		}
		if target.Hostname != "example.com" || len(target.Addresses) != 1 || target.Addresses[0] != "8.8.8.8" {
			validResolved = false
		}
	}

	invalid := []string{
		"",
		"/relative/path",
		"//example.com/scheme-relative",
		"ftp://example.com/file",
		"mailto:security@example.com",
		"https://user:secret@example.com/private",
		"http://[::1",
		"https://",
		"javascript:alert(1)",
		"https://example.com\\@127.0.0.1/",
		"https://example.com/\x00hidden",
	}
	beforeInvalidResolverCalls := resolver.calls
	beforeInvalidProviderCalls := providerCalls
	invalidRejected := true
	invalidTargetErrors := true
	for _, raw := range invalid {
		_, err := gate(raw)
		if err == nil {
			invalidRejected = false
			continue
		}
		if !errors.Is(err, trust.ErrUnsafeInspectionTarget) {
			invalidTargetErrors = false
		}
	}

	out.RecordCounts = map[string]int{
		"reviewed_http_https_forms": len(valid),
		"unsafe_url_forms":          len(invalid),
		"resolver_calls":            resolver.calls,
		"provider_calls":            providerCalls,
	}
	out.Checks = map[string]bool{
		"reviewed_http_https_forms_canonicalize":     validCanonical,
		"reviewed_http_https_forms_resolve_publicly": validResolved,
		"relative_userinfo_scheme_malformed_fail":    invalidRejected,
		"unsafe_forms_use_target_error_authority":    invalidTargetErrors,
		"unsafe_forms_never_reach_dns_resolution":    resolver.calls == beforeInvalidResolverCalls,
		"unsafe_forms_never_reach_provider_access":   providerCalls == beforeInvalidProviderCalls,
		"only_reviewed_forms_reach_provider_gate":    providerCalls == len(valid),
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
