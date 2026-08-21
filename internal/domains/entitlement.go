package domains

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	CustomDomainsCapability = "custom_domains"
	NormalDowngradeGrace    = 7 * 24 * time.Hour
)

var ErrInvalidEntitlementSource = errors.New("invalid custom-domain entitlement source")

type EntitlementSourceKind string

const (
	SourceNone           EntitlementSourceKind = "none"
	SourcePlan           EntitlementSourceKind = "plan"
	SourceManualApproval EntitlementSourceKind = "manual_approval"
)

type EntitlementStatus string

const (
	EntitlementRequested EntitlementStatus = "requested"
	EntitlementActive    EntitlementStatus = "active"
	EntitlementSuspended EntitlementStatus = "suspended"
	EntitlementExpired   EntitlementStatus = "expired"
	EntitlementRevoked   EntitlementStatus = "revoked"
)

type EntitlementSource struct {
	ID               uint64                `json:"id"`
	WorkspaceID      string                `json:"workspace_id"`
	Source           EntitlementSourceKind `json:"source"`
	SourceKey        string                `json:"source_key"`
	Status           EntitlementStatus     `json:"status"`
	DomainLimit      uint32                `json:"domain_limit"`
	StartsAt         time.Time             `json:"starts_at"`
	ExpiresAt        *time.Time            `json:"expires_at,omitempty"`
	DegradedAt       *time.Time            `json:"degraded_at,omitempty"`
	GraceUntil       *time.Time            `json:"grace_until,omitempty"`
	GrantedBy        string                `json:"granted_by,omitempty"`
	SupportTicketID  string                `json:"support_ticket_id,omitempty"`
	DecisionReason   string                `json:"decision_reason,omitempty"`
	SecurityCategory string                `json:"security_category,omitempty"`
}

type AccessRequest struct {
	WorkspaceID     string    `json:"workspace_id"`
	SupportTicketID string    `json:"support_ticket_id"`
	SubmittedAt     time.Time `json:"submitted_at"`
}

type ResolvedEntitlement struct {
	Capability              string                `json:"capability"`
	Source                  EntitlementSourceKind `json:"source"`
	Status                  EntitlementStatus     `json:"status"`
	DomainLimit             uint32                `json:"domain_limit"`
	StartsAt                *time.Time            `json:"starts_at,omitempty"`
	ExpiresAt               *time.Time            `json:"expires_at,omitempty"`
	GrantedBy               string                `json:"granted_by,omitempty"`
	SupportTicketID         string                `json:"support_ticket_id,omitempty"`
	DecisionReason          string                `json:"decision_reason"`
	GracePeriod             bool                  `json:"grace_period"`
	GraceUntil              *time.Time            `json:"grace_until,omitempty"`
	MutationAllowed         bool                  `json:"mutation_allowed"`
	ExistingRoutingAllowed  bool                  `json:"existing_routing_allowed"`
	ValidSources            []EntitlementSource   `json:"valid_sources"`
}

func ValidateEntitlementSource(source EntitlementSource) error {
	if strings.TrimSpace(source.WorkspaceID) == "" || strings.TrimSpace(source.SourceKey) == "" {
		return ErrInvalidEntitlementSource
	}
	if source.Source != SourcePlan && source.Source != SourceManualApproval {
		return ErrInvalidEntitlementSource
	}
	if source.Status != EntitlementActive && source.Status != EntitlementSuspended && source.Status != EntitlementExpired && source.Status != EntitlementRevoked {
		return ErrInvalidEntitlementSource
	}
	if source.DomainLimit == 0 || source.StartsAt.IsZero() {
		return ErrInvalidEntitlementSource
	}
	if source.ExpiresAt != nil && !source.ExpiresAt.After(source.StartsAt) {
		return ErrInvalidEntitlementSource
	}
	if (source.DegradedAt == nil) != (source.GraceUntil == nil) {
		return ErrInvalidEntitlementSource
	}
	if source.DegradedAt != nil {
		if source.Source != SourcePlan || !source.GraceUntil.After(*source.DegradedAt) || source.GraceUntil.Sub(*source.DegradedAt) != NormalDowngradeGrace {
			return ErrInvalidEntitlementSource
		}
	}
	if source.Source == SourceManualApproval {
		if source.ExpiresAt == nil || strings.TrimSpace(source.GrantedBy) == "" || strings.TrimSpace(source.SupportTicketID) == "" || strings.TrimSpace(source.DecisionReason) == "" {
			return ErrInvalidEntitlementSource
		}
	}
	if source.Status == EntitlementSuspended || source.Status == EntitlementRevoked {
		if strings.TrimSpace(source.DecisionReason) == "" || strings.TrimSpace(source.SecurityCategory) == "" {
			return ErrInvalidEntitlementSource
		}
	}
	return nil
}

func ResolveEntitlement(now time.Time, sources []EntitlementSource, request *AccessRequest) (ResolvedEntitlement, error) {
	now = now.UTC()
	for index := range sources {
		if err := ValidateEntitlementSource(sources[index]); err != nil {
			return ResolvedEntitlement{}, fmt.Errorf("source %d: %w", index, err)
		}
	}

	if security := strongestCurrentSecuritySource(now, sources); security != nil {
		return resolvedFromSource(*security, security.Status, false, false, false, nil, []EntitlementSource{*security}), nil
	}

	regular := make([]EntitlementSource, 0, len(sources))
	grace := make([]EntitlementSource, 0, len(sources))
	for _, source := range sources {
		if source.Status != EntitlementActive || now.Before(source.StartsAt.UTC()) {
			continue
		}
		if source.ExpiresAt != nil && !now.Before(source.ExpiresAt.UTC()) {
			continue
		}
		if source.DegradedAt != nil {
			if now.Before(source.DegradedAt.UTC()) {
				regular = append(regular, source)
				continue
			}
			if now.Before(source.GraceUntil.UTC()) {
				grace = append(grace, source)
			}
			continue
		}
		regular = append(regular, source)
	}

	if len(regular) > 0 {
		selected := highestLimitSource(regular)
		return resolvedFromSource(selected, EntitlementActive, true, true, false, nil, regular), nil
	}

	if len(grace) > 0 {
		selected := highestLimitSource(grace)
		return resolvedFromSource(selected, EntitlementActive, false, true, true, selected.GraceUntil, grace), nil
	}

	if request != nil && strings.TrimSpace(request.SupportTicketID) != "" {
		return ResolvedEntitlement{
			Capability:             CustomDomainsCapability,
			Source:                 SourceNone,
			Status:                 EntitlementRequested,
			SupportTicketID:        request.SupportTicketID,
			DecisionReason:         "support_request_pending_independent_approval",
			MutationAllowed:        false,
			ExistingRoutingAllowed: false,
			ValidSources:           []EntitlementSource{},
		}, nil
	}

	return ResolvedEntitlement{
		Capability:             CustomDomainsCapability,
		Source:                 SourceNone,
		Status:                 EntitlementExpired,
		DecisionReason:         "not_entitled",
		MutationAllowed:        false,
		ExistingRoutingAllowed: false,
		ValidSources:           []EntitlementSource{},
	}, nil
}

func strongestCurrentSecuritySource(now time.Time, sources []EntitlementSource) *EntitlementSource {
	var selected *EntitlementSource
	for index := range sources {
		source := &sources[index]
		if source.Status != EntitlementSuspended && source.Status != EntitlementRevoked {
			continue
		}
		if now.Before(source.StartsAt.UTC()) {
			continue
		}
		if source.ExpiresAt != nil && !now.Before(source.ExpiresAt.UTC()) {
			continue
		}
		if selected == nil || securityPriority(source.Status) > securityPriority(selected.Status) {
			copy := *source
			selected = &copy
		}
	}
	return selected
}

func securityPriority(status EntitlementStatus) int {
	if status == EntitlementRevoked {
		return 2
	}
	if status == EntitlementSuspended {
		return 1
	}
	return 0
}

func highestLimitSource(sources []EntitlementSource) EntitlementSource {
	ordered := append([]EntitlementSource(nil), sources...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].DomainLimit != ordered[j].DomainLimit {
			return ordered[i].DomainLimit > ordered[j].DomainLimit
		}
		if ordered[i].Source != ordered[j].Source {
			return ordered[i].Source == SourcePlan
		}
		return ordered[i].SourceKey < ordered[j].SourceKey
	})
	return ordered[0]
}

func resolvedFromSource(source EntitlementSource, status EntitlementStatus, mutationAllowed, routingAllowed, grace bool, graceUntil *time.Time, validSources []EntitlementSource) ResolvedEntitlement {
	starts := source.StartsAt.UTC()
	var expires *time.Time
	if source.ExpiresAt != nil {
		value := source.ExpiresAt.UTC()
		expires = &value
	}
	var normalizedGrace *time.Time
	if graceUntil != nil {
		value := graceUntil.UTC()
		normalizedGrace = &value
	}
	reason := strings.TrimSpace(source.DecisionReason)
	if reason == "" {
		if grace {
			reason = "normal_plan_downgrade_grace"
		} else if source.Source == SourcePlan {
			reason = "active_plan_entitlement"
		} else {
			reason = "active_manual_approval"
		}
	}
	return ResolvedEntitlement{
		Capability:             CustomDomainsCapability,
		Source:                 source.Source,
		Status:                 status,
		DomainLimit:            source.DomainLimit,
		StartsAt:               &starts,
		ExpiresAt:              expires,
		GrantedBy:              source.GrantedBy,
		SupportTicketID:        source.SupportTicketID,
		DecisionReason:         reason,
		GracePeriod:            grace,
		GraceUntil:             normalizedGrace,
		MutationAllowed:        mutationAllowed,
		ExistingRoutingAllowed: routingAllowed,
		ValidSources:           append([]EntitlementSource(nil), validSources...),
	}
}
