package links

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

const riskFingerprintVersion = "gojet-v10-risk-targets-v1"

var (
	ErrInvalidDestination = errors.New("invalid destination")
	ErrInvalidABWeights   = errors.New("invalid A/B weights")
	ErrInvalidInput       = errors.New("invalid link input")
	ErrNotFound           = errors.New("link not found")
	ErrConflict           = errors.New("link version conflict")
)

type RoutingRule struct {
	ID          string `json:"id"`
	MatchType   string `json:"match_type"`
	MatchValue  string `json:"match_value"`
	Destination string `json:"destination"`
	Enabled     bool   `json:"enabled"`
}

type ABVariant struct {
	ID          string `json:"id"`
	Destination string `json:"destination"`
	Weight      int    `json:"weight"`
	Enabled     bool   `json:"enabled"`
}

type UTMConfig struct {
	Source   string `json:"source,omitempty"`
	Medium   string `json:"medium,omitempty"`
	Campaign string `json:"campaign,omitempty"`
	Term     string `json:"term,omitempty"`
	Content  string `json:"content,omitempty"`
}

type AccessConfig struct {
	PasswordHash string `json:"password_hash,omitempty"`
}

type Link struct {
	ID                 uint64        `json:"id"`
	WorkspaceID        string        `json:"workspace_id"`
	Hostname           string        `json:"hostname"`
	DomainKind         string        `json:"domain_kind"`
	Code               string        `json:"code"`
	Title              string        `json:"title"`
	PrimaryDestination string        `json:"primary_destination"`
	RedirectStatus     int           `json:"redirect_status"`
	Status             string        `json:"status"`
	Version            uint64        `json:"version"`
	RiskFingerprint    string        `json:"risk_fingerprint"`
	Routing            []RoutingRule `json:"routing"`
	AB                 []ABVariant   `json:"ab"`
	UTM                UTMConfig     `json:"utm"`
	Access             AccessConfig  `json:"access"`
	ExpiresAt          *time.Time    `json:"expires_at,omitempty"`
	ClickLimit         *uint64       `json:"click_limit,omitempty"`
	ClickCount         uint64        `json:"click_count"`
	OneTime            bool          `json:"one_time"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
	DeletedAt          *time.Time    `json:"deleted_at,omitempty"`
}

// NormalizeDestination returns the canonical URL representation used by the
// destination-risk fingerprint. Only absolute HTTP(S) destinations are valid.
func NormalizeDestination(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrInvalidDestination
	}

	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Hostname() == "" {
		return "", ErrInvalidDestination
	}

	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", ErrInvalidDestination
	}
	if u.User != nil {
		return "", ErrInvalidDestination
	}
	if u.Fragment != "" {
		u.Fragment = ""
	}

	hostname := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if hostname == "" {
		return "", ErrInvalidDestination
	}
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	if port != "" {
		u.Host = net.JoinHostPort(hostname, port)
	} else if ip := net.ParseIP(hostname); ip != nil && strings.Contains(hostname, ":") {
		u.Host = "[" + hostname + "]"
	} else {
		u.Host = hostname
	}

	if u.Path == "" {
		u.Path = "/"
	} else {
		cleaned := path.Clean(u.EscapedPath())
		if cleaned == "." {
			cleaned = "/"
		}
		if !strings.HasPrefix(cleaned, "/") {
			cleaned = "/" + cleaned
		}
		decoded, decodeErr := url.PathUnescape(cleaned)
		if decodeErr != nil {
			return "", ErrInvalidDestination
		}
		u.Path = decoded
		u.RawPath = ""
	}

	query := u.Query()
	u.RawQuery = query.Encode()

	return u.String(), nil
}

// ReachableTargetSet returns the sorted de-duplicated set of every destination
// that redirectengine can reach before UTM mutation: primary, enabled routing
// targets and enabled A/B variants.
func ReachableTargetSet(primary string, routing []RoutingRule, variants []ABVariant) ([]string, error) {
	set := make(map[string]struct{}, 1+len(routing)+len(variants))
	add := func(raw string) error {
		normalized, err := NormalizeDestination(raw)
		if err != nil {
			return err
		}
		set[normalized] = struct{}{}
		return nil
	}

	if err := add(primary); err != nil {
		return nil, fmt.Errorf("primary destination: %w", err)
	}
	for _, rule := range routing {
		if !rule.Enabled {
			continue
		}
		if err := add(rule.Destination); err != nil {
			return nil, fmt.Errorf("routing rule %q: %w", rule.ID, err)
		}
	}
	for _, variant := range variants {
		if !variant.Enabled {
			continue
		}
		if err := add(variant.Destination); err != nil {
			return nil, fmt.Errorf("A/B variant %q: %w", variant.ID, err)
		}
	}

	targets := make([]string, 0, len(set))
	for target := range set {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	return targets, nil
}

func RiskFingerprint(primary string, routing []RoutingRule, variants []ABVariant) (string, []string, error) {
	targets, err := ReachableTargetSet(primary, routing, variants)
	if err != nil {
		return "", nil, err
	}

	canonical := riskFingerprintVersion + "\n" + strings.Join(targets, "\n") + "\n"
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:]), targets, nil
}

// ValidateABWeights requires enabled variants to use positive integer weights
// whose total is exactly 100. Disabled variants do not participate.
func ValidateABWeights(variants []ABVariant) error {
	enabled := 0
	total := 0
	ids := map[string]struct{}{}
	for _, variant := range variants {
		if !variant.Enabled {
			continue
		}
		enabled++
		if variant.ID == "" || variant.Weight <= 0 {
			return ErrInvalidABWeights
		}
		if _, exists := ids[variant.ID]; exists {
			return ErrInvalidABWeights
		}
		ids[variant.ID] = struct{}{}
		total += variant.Weight
	}
	if enabled == 0 {
		return nil
	}
	if enabled < 2 || total != 100 {
		return ErrInvalidABWeights
	}
	return nil
}
