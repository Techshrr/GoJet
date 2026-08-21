package domains

import (
	"net"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// HostnamePolicy is the single P06 authority for customer hostname
// normalization and platform-host exclusion. Persisted identity is always the
// normalized ASCII form; DisplayHostname is a round-tripped, safe display form.
type HostnamePolicy struct {
	platformHosts map[string]struct{}
}

type NormalizedHostname struct {
	ASCII   string `json:"ascii"`
	Display string `json:"display"`
}

var goJetHostnamePolicy = mustHostnamePolicy([]string{
	"gojet.cc",
	"gojet.cn",
})

// GoJetHostnamePolicy returns the server-owned hostname authority used by P06
// domain mutations. Client requests provide only the candidate hostname; they
// cannot provide, replace or weaken the platform-host exclusion set.
func GoJetHostnamePolicy() HostnamePolicy {
	return goJetHostnamePolicy
}

func mustHostnamePolicy(platformHostnames []string) HostnamePolicy {
	policy, err := NewHostnamePolicy(platformHostnames)
	if err != nil {
		panic("invalid server-owned GoJet platform hostname authority: " + err.Error())
	}
	return policy
}

func NewHostnamePolicy(platformHostnames []string) (HostnamePolicy, error) {
	policy := HostnamePolicy{platformHosts: map[string]struct{}{}}
	for _, raw := range platformHostnames {
		normalized, err := normalizeIDNAHostname(raw, false)
		if err != nil {
			return HostnamePolicy{}, err
		}
		policy.platformHosts[normalized.ASCII] = struct{}{}
	}
	return policy, nil
}

func (p HostnamePolicy) Normalize(raw string) (NormalizedHostname, error) {
	normalized, err := normalizeIDNAHostname(raw, true)
	if err != nil {
		return NormalizedHostname{}, err
	}
	for platformHost := range p.platformHosts {
		if normalized.ASCII == platformHost || strings.HasSuffix(normalized.ASCII, "."+platformHost) {
			return NormalizedHostname{}, ErrInvalidHostname
		}
	}
	return normalized, nil
}

func normalizeIDNAHostname(raw string, requireRegistrable bool) (NormalizedHostname, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimSuffix(trimmed, ".")
	if trimmed == "" || strings.HasPrefix(trimmed, "*.") || strings.ContainsAny(trimmed, "/?#@ ") {
		return NormalizedHostname{}, ErrInvalidHostname
	}
	if net.ParseIP(trimmed) != nil || strings.EqualFold(trimmed, "localhost") {
		return NormalizedHostname{}, ErrInvalidHostname
	}

	ascii, err := idna.Lookup.ToASCII(trimmed)
	if err != nil {
		return NormalizedHostname{}, ErrInvalidHostname
	}
	ascii = strings.ToLower(strings.TrimSuffix(ascii, "."))
	if ascii == "" || len(ascii) > 253 || net.ParseIP(ascii) != nil {
		return NormalizedHostname{}, ErrInvalidHostname
	}

	if requireRegistrable {
		suffix, icann := publicsuffix.PublicSuffix(ascii)
		if suffix == "" {
			return NormalizedHostname{}, ErrInvalidHostname
		}
		// Unknown/reserved single-label suffixes such as .example/.invalid/.test
		// are not accepted. A known private suffix may contain a dot; the caller
		// must still prove TXT ownership before activation.
		if !icann && !strings.Contains(suffix, ".") {
			return NormalizedHostname{}, ErrInvalidHostname
		}
		registrable, registrableErr := publicsuffix.EffectiveTLDPlusOne(ascii)
		if registrableErr != nil || registrable == "" || ascii == suffix {
			return NormalizedHostname{}, ErrInvalidHostname
		}
	}

	display, err := idna.Lookup.ToUnicode(ascii)
	if err != nil || display == "" {
		return NormalizedHostname{}, ErrInvalidHostname
	}
	for _, r := range display {
		if unicode.IsControl(r) || r == '\u202a' || r == '\u202b' || r == '\u202c' || r == '\u202d' || r == '\u202e' || r == '\u2066' || r == '\u2067' || r == '\u2068' || r == '\u2069' {
			return NormalizedHostname{}, ErrInvalidHostname
		}
	}
	roundTrip, err := idna.Lookup.ToASCII(display)
	if err != nil || strings.ToLower(roundTrip) != ascii {
		return NormalizedHostname{}, ErrInvalidHostname
	}

	return NormalizedHostname{ASCII: ascii, Display: display}, nil
}
