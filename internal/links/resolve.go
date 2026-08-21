package links

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

type ResolveContext struct {
	Country        string
	Device         string
	Language       string
	SourceHostname string
	ABSeed         string
}

type SelectedTarget struct {
	Destination string `json:"destination"`
	Source      string `json:"source"` // primary | routing | ab
	SourceID    string `json:"source_id,omitempty"`
}

// SelectTarget applies the P05 deterministic product rule after risk allow:
// first matching routing rule wins; when no routing rule matches, enabled A/B
// variants are evaluated; otherwise the primary destination is used.
func SelectTarget(link Link, request ResolveContext) (SelectedTarget, error) {
	for _, rule := range link.Routing {
		if !rule.Enabled || !routingRuleMatches(rule, request) {
			continue
		}
		destination, err := NormalizeDestination(rule.Destination)
		if err != nil {
			return SelectedTarget{}, err
		}
		return SelectedTarget{Destination: destination, Source: "routing", SourceID: rule.ID}, nil
	}

	enabled := make([]ABVariant, 0, len(link.AB))
	for _, variant := range link.AB {
		if variant.Enabled {
			enabled = append(enabled, variant)
		}
	}
	if len(enabled) > 0 {
		if err := ValidateABWeights(enabled); err != nil {
			return SelectedTarget{}, err
		}
		variant := chooseABVariant(link.ID, enabled, request.ABSeed)
		destination, err := NormalizeDestination(variant.Destination)
		if err != nil {
			return SelectedTarget{}, err
		}
		return SelectedTarget{Destination: destination, Source: "ab", SourceID: variant.ID}, nil
	}

	destination, err := NormalizeDestination(link.PrimaryDestination)
	if err != nil {
		return SelectedTarget{}, err
	}
	return SelectedTarget{Destination: destination, Source: "primary"}, nil
}

func routingRuleMatches(rule RoutingRule, request ResolveContext) bool {
	want := strings.TrimSpace(rule.MatchValue)
	if want == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(rule.MatchType)) {
	case "country":
		return strings.EqualFold(strings.TrimSpace(request.Country), want)
	case "device":
		return strings.EqualFold(strings.TrimSpace(request.Device), want)
	case "language":
		actual := strings.ToLower(strings.TrimSpace(request.Language))
		expected := strings.ToLower(want)
		return actual == expected || strings.HasPrefix(actual, expected+"-")
	case "source":
		return strings.EqualFold(strings.TrimSpace(request.SourceHostname), strings.TrimSuffix(strings.ToLower(want), "."))
	default:
		return false
	}
}

func chooseABVariant(linkID uint64, variants []ABVariant, seed string) ABVariant {
	ordered := append([]ABVariant(nil), variants...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	material := fmt.Sprintf("gojet-v10-ab-v1\n%d\n%s", linkID, seed)
	sum := sha256.Sum256([]byte(material))
	bucket := int(binary.BigEndian.Uint64(sum[:8]) % 100)
	cursor := 0
	for _, variant := range ordered {
		cursor += variant.Weight
		if bucket < cursor {
			return variant
		}
	}
	return ordered[len(ordered)-1]
}

func ApplyUTM(rawDestination string, config UTMConfig) (string, error) {
	destination, err := NormalizeDestination(rawDestination)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(destination)
	if err != nil {
		return "", err
	}
	query := u.Query()
	set := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			query.Set(key, value)
		}
	}
	set("utm_source", config.Source)
	set("utm_medium", config.Medium)
	set("utm_campaign", config.Campaign)
	set("utm_term", config.Term)
	set("utm_content", config.Content)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func VerifySelectedTargetIsFingerprintMember(link Link, selected string) error {
	normalized, err := NormalizeDestination(selected)
	if err != nil {
		return err
	}
	_, targets, err := RiskFingerprint(link.PrimaryDestination, link.Routing, link.AB)
	if err != nil {
		return err
	}
	index := sort.SearchStrings(targets, normalized)
	if index >= len(targets) || targets[index] != normalized {
		return errors.New("selected destination is not a member of the current risk fingerprint target set")
	}
	return nil
}
