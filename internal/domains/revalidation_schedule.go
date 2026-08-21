package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type RevalidationAxis string

const (
	RevalidationEntitlement RevalidationAxis = "entitlement"
	RevalidationOwnership   RevalidationAxis = "ownership"
	RevalidationIngressDNS  RevalidationAxis = "ingress_dns"
	RevalidationHTTPS       RevalidationAxis = "https"
	RevalidationRisk        RevalidationAxis = "risk"
)

type RevalidationSchedulePolicy struct {
	Version             string
	EntitlementInterval time.Duration
	OwnershipInterval   time.Duration
	IngressDNSInterval  time.Duration
	HTTPSInterval       time.Duration
	RiskInterval        time.Duration
}

type revalidationSchedule struct {
	PolicyVersion string
	NextDueAt     time.Time
}

func (p RevalidationSchedulePolicy) schedule(axis RevalidationAxis, checkedAt time.Time) (*revalidationSchedule, error) {
	version := strings.TrimSpace(p.Version)
	if version == "" || checkedAt.IsZero() {
		return nil, ErrInvalidDomainMutation
	}
	var interval time.Duration
	switch axis {
	case RevalidationEntitlement:
		interval = p.EntitlementInterval
	case RevalidationOwnership:
		interval = p.OwnershipInterval
	case RevalidationIngressDNS:
		interval = p.IngressDNSInterval
	case RevalidationHTTPS:
		interval = p.HTTPSInterval
	case RevalidationRisk:
		interval = p.RiskInterval
	default:
		return nil, ErrInvalidDomainMutation
	}
	if interval <= 0 {
		return nil, ErrInvalidDomainMutation
	}
	return &revalidationSchedule{
		PolicyVersion: version,
		NextDueAt:     checkedAt.UTC().Add(interval),
	}, nil
}

func appendDomainRevalidationTx(
	ctx context.Context,
	tx *sql.Tx,
	domain Domain,
	axis RevalidationAxis,
	result string,
	policyVersion string,
	checkedAt time.Time,
	schedule *revalidationSchedule,
	evidenceRef string,
	correlationID string,
	metadata map[string]any,
) error {
	policyVersion = strings.TrimSpace(policyVersion)
	evidenceRef = strings.TrimSpace(evidenceRef)
	correlationID = strings.TrimSpace(correlationID)
	if tx == nil || domain.ID == 0 || domain.WorkspaceID == "" || policyVersion == "" || checkedAt.IsZero() || correlationID == "" || metadata == nil {
		return ErrInvalidDomainMutation
	}
	switch axis {
	case RevalidationEntitlement, RevalidationOwnership, RevalidationIngressDNS, RevalidationHTTPS, RevalidationRisk:
	default:
		return ErrInvalidDomainMutation
	}
	switch result {
	case "pass", "fail", "pending", "stale", "error":
	default:
		return ErrInvalidDomainMutation
	}

	var previousChecked sql.NullTime
	err := tx.QueryRowContext(ctx, `
		SELECT checked_at
		FROM custom_domain_revalidations
		WHERE domain_id = ? AND workspace_id = ? AND axis = ?
		ORDER BY id DESC
		LIMIT 1`, domain.ID, domain.WorkspaceID, string(axis)).Scan(&previousChecked)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previousChecked.Valid {
		metadata["previous_checked_at"] = previousChecked.Time.UTC().Format(time.RFC3339Nano)
	}

	var nextDue any
	if schedule != nil {
		if strings.TrimSpace(schedule.PolicyVersion) == "" || schedule.NextDueAt.IsZero() || !schedule.NextDueAt.After(checkedAt) {
			return ErrInvalidDomainMutation
		}
		nextDue = schedule.NextDueAt.UTC()
		metadata["trigger"] = "periodic"
		metadata["schedule_policy_version"] = schedule.PolicyVersion
		metadata["next_due_at"] = schedule.NextDueAt.UTC().Format(time.RFC3339Nano)
	}

	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_revalidations (
			domain_id, workspace_id, axis, result, policy_version, checked_at,
			next_due_at, evidence_ref, correlation_id, metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		domain.ID, domain.WorkspaceID, string(axis), result, policyVersion, checkedAt.UTC(),
		nextDue, evidenceRef, correlationID, string(raw),
	)
	return err
}
