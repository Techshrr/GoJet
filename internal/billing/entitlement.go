package billing

import (
	"fmt"
	"sort"
	"time"
)

type ResolvedEntitlement struct {
	WorkspaceID  string                `json:"workspace_id"`
	Capability   string                `json:"capability"`
	Allowed      bool                  `json:"allowed"`
	LimitValue   uint64                `json:"limit_value"`
	SourceType   EntitlementSourceType `json:"source_type"`
	SourceID     string                `json:"source_id"`
	Reason       string                `json:"reason"`
	ActiveGrants []EntitlementGrant    `json:"active_grants"`
}

func ResolveEntitlement(now time.Time, workspaceID, capability string, grants []EntitlementGrant) (ResolvedEntitlement, error) {
	now = now.UTC()
	active := make([]EntitlementGrant, 0, len(grants))
	denies := make([]EntitlementGrant, 0, 1)
	for i, grant := range grants {
		if err := grant.Validate(); err != nil {
			return ResolvedEntitlement{}, fmt.Errorf("grant %d: %w", i, err)
		}
		if grant.WorkspaceID != workspaceID || grant.Capability != capability || now.Before(grant.StartsAt.UTC()) {
			continue
		}
		if grant.RevokedAt != nil && !now.Before(grant.RevokedAt.UTC()) {
			continue
		}
		if grant.EndsAt != nil && !now.Before(grant.EndsAt.UTC()) {
			continue
		}
		if grant.SourceType == SourceHardDeny {
			denies = append(denies, grant)
			continue
		}
		active = append(active, grant)
	}
	if len(denies) > 0 {
		sort.SliceStable(denies, func(i, j int) bool { return denies[i].SourceID < denies[j].SourceID })
		return ResolvedEntitlement{
			WorkspaceID: workspaceID, Capability: capability, Allowed: false,
			SourceType: SourceHardDeny, SourceID: denies[0].SourceID,
			Reason: "hard_deny", ActiveGrants: append([]EntitlementGrant(nil), active...),
		}, nil
	}
	if len(active) == 0 {
		return ResolvedEntitlement{
			WorkspaceID: workspaceID, Capability: capability, Allowed: false,
			SourceType: SourceBaseline, Reason: "not_entitled", ActiveGrants: []EntitlementGrant{},
		}, nil
	}
	ordered := append([]EntitlementGrant(nil), active...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].LimitValue != ordered[j].LimitValue {
			return ordered[i].LimitValue > ordered[j].LimitValue
		}
		if sourcePriority(ordered[i].SourceType) != sourcePriority(ordered[j].SourceType) {
			return sourcePriority(ordered[i].SourceType) > sourcePriority(ordered[j].SourceType)
		}
		return ordered[i].SourceID < ordered[j].SourceID
	})
	selected := ordered[0]
	return ResolvedEntitlement{
		WorkspaceID: workspaceID, Capability: capability, Allowed: true,
		LimitValue: selected.LimitValue, SourceType: selected.SourceType, SourceID: selected.SourceID,
		Reason: "active_grant", ActiveGrants: ordered,
	}, nil
}

func sourcePriority(source EntitlementSourceType) int {
	switch source {
	case SourceManual:
		return 4
	case SourceInherited:
		return 3
	case SourceBilling:
		return 2
	case SourceBaseline:
		return 1
	default:
		return 0
	}
}
