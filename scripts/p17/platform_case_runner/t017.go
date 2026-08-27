package main

import (
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT017(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL official-host lifecycle with domains.manage, default/HTTPS safety and P16 risk parity kept separate from P17 host configuration")
	now := time.Date(2026, 8, 28, 1, 10, 0, 0, time.UTC)
	service, root, _, err := bootstrapRoot(ctx, runtime, now)
	if err != nil {
		return out, err
	}
	item, replayed, err := service.CreateOfficialDomain(ctx, root, adminaccess.CreateOfficialDomainInput{Hostname: "GoJet.CC."}, adminaccess.MutationAuthority{Reason: "reviewed official host", CorrelationID: "p17-t017-create", IdempotencyKey: "p17-t017-create-key"}, now.Add(time.Second))
	if err != nil {
		return out, err
	}
	item, _, err = service.MutateOfficialDomain(ctx, root, item.ID, adminaccess.OfficialDomainActionInput{Action: "https_active", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "verified https active", CorrelationID: "p17-t017-https", IdempotencyKey: "p17-t017-https-key"}, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	item, _, err = service.MutateOfficialDomain(ctx, root, item.ID, adminaccess.OfficialDomainActionInput{Action: "set_default", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "set reviewed default", CorrelationID: "p17-t017-default", IdempotencyKey: "p17-t017-default-key"}, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	_, _, disableDefaultErr := service.MutateOfficialDomain(ctx, root, item.ID, adminaccess.OfficialDomainActionInput{Action: "disable", ExpectedVersion: item.Version}, adminaccess.MutationAuthority{Reason: "default disable safety probe", CorrelationID: "p17-t017-disable", IdempotencyKey: "p17-t017-disable-key"}, now.Add(4*time.Second))
	_, domainRiskOnly, _, err := createScopedMFAAdmin(ctx, service, root, "T017", "domain-risk-only", adminaccess.PermissionDomainsRiskManage, now.Add(5*time.Second))
	if err != nil {
		return out, err
	}
	_, deniedErr := service.ListOfficialDomains(ctx, domainRiskOnly)
	if _, err := runtime.DB.ExecContext(ctx, `INSERT INTO custom_domains(workspace_id,hostname_ascii,display_hostname,routing_state,ownership_status,ingress_dns_status,https_status,risk_status,created_at,updated_at) VALUES ('ws-p17-t017','blocked.official.test','blocked.official.test','suspended','verified','valid','active','block',?,?)`, now, now); err != nil {
		return out, err
	}
	var p16RiskStatus, p16RoutingState string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT risk_status,routing_state FROM custom_domains WHERE workspace_id='ws-p17-t017'`).Scan(&p16RiskStatus, &p16RoutingState); err != nil {
		return out, err
	}
	items, err := service.ListOfficialDomains(ctx, root)
	if err != nil {
		return out, err
	}
	var p16RiskAfter, p16RoutingAfter string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT risk_status,routing_state FROM custom_domains WHERE workspace_id='ws-p17-t017'`).Scan(&p16RiskAfter, &p16RoutingAfter); err != nil {
		return out, err
	}
	out.RecordCounts["official_domains"] = len(items)
	out.Checks["domains_manage_required_and_risk_permission_independent"] = errors.Is(deniedErr, adminaccess.ErrForbidden)
	out.Checks["hostname_normalized"] = item.Hostname == "gojet.cc" && !replayed
	out.Checks["https_then_default_lifecycle_valid"] = item.HTTPSState == "active" && item.IsDefault && item.Enabled
	out.Checks["default_host_cannot_be_disabled"] = errors.Is(disableDefaultErr, adminaccess.ErrConflict)
	out.Checks["p16_risk_parity_not_reinterpreted"] = p16RiskStatus == "block" && p16RoutingState == "suspended" && p16RiskAfter == p16RiskStatus && p16RoutingAfter == p16RoutingState
	out.Checks["official_host_projection_remains_separate"] = len(items) == 1 && items[0].Hostname == "gojet.cc"
	pass(&out)
	return out, nil
}
