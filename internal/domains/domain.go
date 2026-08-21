package domains

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"time"
)

var (
	ErrInvalidHostname       = errors.New("invalid custom-domain hostname")
	ErrInvalidDomainMutation = errors.New("invalid custom-domain mutation")
	ErrEntitlementRequired   = errors.New("custom-domain entitlement required")
	ErrOwnershipRequired     = errors.New("custom-domain ownership verification required")
	ErrDomainLimitReached    = errors.New("custom-domain limit reached")
	ErrHostnameConflict      = errors.New("custom-domain hostname unavailable")
	ErrDomainNotFound        = errors.New("custom domain not found")
	ErrOwnershipDNSLookup    = errors.New("custom-domain ownership DNS lookup failed")
	ErrIngressDNSLookup      = errors.New("custom-domain ingress DNS lookup failed")
)

type RoutingState string

type OwnershipStatus string

type IngressDNSStatus string

type HTTPSStatus string

type DomainRiskStatus string

const (
	RoutingPending   RoutingState = "pending"
	RoutingEnabled   RoutingState = "enabled"
	RoutingSuspended RoutingState = "suspended"
	RoutingRevoked   RoutingState = "revoked"
	RoutingRemoved   RoutingState = "removed"

	OwnershipPending  OwnershipStatus = "pending"
	OwnershipVerified OwnershipStatus = "verified"
	OwnershipFailed   OwnershipStatus = "failed"
	OwnershipLost     OwnershipStatus = "lost"

	IngressPending IngressDNSStatus = "pending"
	IngressValid   IngressDNSStatus = "valid"
	IngressInvalid IngressDNSStatus = "invalid"

	HTTPSPending HTTPSStatus = "pending"
	HTTPSActive  HTTPSStatus = "active"
	HTTPSError   HTTPSStatus = "error"

	RiskMissing   DomainRiskStatus = "missing"
	RiskAllow     DomainRiskStatus = "allow"
	RiskReview    DomainRiskStatus = "review"
	RiskBlock     DomainRiskStatus = "block"
	RiskMalformed DomainRiskStatus = "malformed"
	RiskStale     DomainRiskStatus = "stale"
)

type Domain struct {
	ID                      uint64           `json:"id"`
	WorkspaceID             string           `json:"workspace_id"`
	HostnameASCII           string           `json:"hostname_ascii"`
	DisplayHostname         string           `json:"display_hostname"`
	RoutingState            RoutingState     `json:"routing_state"`
	OwnershipStatus         OwnershipStatus  `json:"ownership_status"`
	IngressDNSStatus        IngressDNSStatus `json:"ingress_dns_status"`
	HTTPSStatus             HTTPSStatus      `json:"https_status"`
	RiskStatus              DomainRiskStatus `json:"risk_status"`
	OwnershipTokenVersion   uint64           `json:"ownership_token_version"`
	OwnershipSecretIssuedAt time.Time        `json:"ownership_secret_issued_at"`
	OwnershipVerifiedAt     *time.Time       `json:"ownership_verified_at,omitempty"`
	IngressDNSCheckedAt     *time.Time       `json:"ingress_dns_checked_at,omitempty"`
	HTTPSCheckedAt          *time.Time       `json:"https_checked_at,omitempty"`
	RiskCheckedAt           *time.Time       `json:"risk_checked_at,omitempty"`
	RiskPolicyVersion       string           `json:"risk_policy_version,omitempty"`
	RiskEvidenceRef         string           `json:"risk_evidence_ref,omitempty"`
	GraceStartedAt          *time.Time       `json:"grace_started_at,omitempty"`
	GraceUntil              *time.Time       `json:"grace_until,omitempty"`
	SecurityCategory        string           `json:"security_category,omitempty"`
	CreatedAt               time.Time        `json:"created_at"`
	UpdatedAt               time.Time        `json:"updated_at"`
	RemovedAt               *time.Time       `json:"removed_at,omitempty"`
}

type DomainReadiness struct {
	EntitlementReady bool `json:"entitlement_ready"`
	OwnershipReady   bool `json:"ownership_ready"`
	IngressDNSReady  bool `json:"ingress_dns_ready"`
	HTTPSReady       bool `json:"https_ready"`
	RiskReady        bool `json:"risk_ready"`
	ReadyForNewLinks bool `json:"ready_for_new_links"`
	ReadyForRouting  bool `json:"ready_for_routing"`
}

func (d Domain) Readiness(entitlement ResolvedEntitlement) DomainReadiness {
	ownership := d.OwnershipStatus == OwnershipVerified
	dns := d.IngressDNSStatus == IngressValid
	https := d.HTTPSStatus == HTTPSActive
	risk := d.RiskStatus == RiskAllow
	routingStateAllows := d.RoutingState != RoutingSuspended && d.RoutingState != RoutingRevoked && d.RoutingState != RoutingRemoved
	trust := ownership && dns && https && risk && routingStateAllows
	return DomainReadiness{
		EntitlementReady: entitlement.MutationAllowed,
		OwnershipReady:   ownership,
		IngressDNSReady:  dns,
		HTTPSReady:       https,
		RiskReady:        risk,
		ReadyForNewLinks: entitlement.MutationAllowed && trust,
		ReadyForRouting:  entitlement.ExistingRoutingAllowed && trust,
	}
}

func NewOwnershipSecret() (plaintext string, hash [32]byte, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", [32]byte{}, err
	}
	plaintext = base64.RawURLEncoding.EncodeToString(raw)
	hash = sha256.Sum256([]byte(plaintext))
	return plaintext, hash, nil
}

func OwnershipSecretMatches(plaintext string, expected [32]byte) bool {
	actual := sha256.Sum256([]byte(plaintext))
	return subtle.ConstantTimeCompare(actual[:], expected[:]) == 1
}

func OwnershipTXTName(hostnameASCII string) string {
	return "_gojet-verification." + hostnameASCII
}

func OwnershipTXTValue(secret string) string {
	return "gojet-verification=" + secret
}
