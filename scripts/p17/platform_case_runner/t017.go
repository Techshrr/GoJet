package main

import (
	"context"
	"errors"
	"time"

	adminaccess "github.com/Techshrr/GoJet/internal/admin"
	"github.com/Techshrr/GoJet/scripts/p17/adminfixture"
)

func runT017(ctx context.Context, runtime *adminfixture.Runtime) (output, error) {
	out := newOutput("real MySQL official-domain lifecycle with domains.manage, default-conflict and invariant P05/P16 risk projections")
	now := time.Date(2026, 8, 28, 1, 10, 0, 0, time.UTC)
	service, root, _, err := bootstrapCaseRoot(ctx, runtime, "T017", []string{adminaccess.PermissionDomainsManage}, now)
	if err != nil {
		return out, err
	}
	other, _, err := createScopedMFAAdmin(ctx, service, root, "T017", "settings-only", adminaccess.PermissionSettingsManage, now.Add(2*time.Second))
	if err != nil {
		return out, err
	}
	_, err = runtime.DB.ExecContext(ctx, `INSERT INTO links(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json) VALUES ('ws-p17-t017','gojet.cc','official','official-risk-probe','','https://example.com',302,'active',1,REPEAT('a',64),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT(),JSON_OBJECT())`)
	if err != nil {
		return out, err
	}
	var riskBefore string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT risk_fingerprint FROM links WHERE code='official-risk-probe'`).Scan(&riskBefore); err != nil {
		return out, err
	}
	var p16Before int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM destination_risk_decisions`).Scan(&p16Before); err != nil {
		return out, err
	}
	primary, _, err := service.CreateOfficialDomain(ctx, root, adminaccess.CreateOfficialDomainInput{Hostname: "gojet.cc"}, adminaccess.MutationAuthority{Reason: "register reviewed official host", CorrelationID: "p17-t017-create", IdempotencyKey: "p17-t017-create-key"}, now.Add(3*time.Second))
	if err != nil {
		return out, err
	}
	primary, _, err = service.MutateOfficialDomain(ctx, root, primary.ID, adminaccess.OfficialDomainActionInput{Action: "https_active", ExpectedVersion: primary.Version}, adminaccess.MutationAuthority{Reason: "activate reviewed https state", CorrelationID: "p17-t017-https", IdempotencyKey: "p17-t017-https-key"}, now.Add(4*time.Second))
	if err != nil {
		return out, err
	}
	primary, _, err = service.MutateOfficialDomain(ctx, root, primary.ID, adminaccess.OfficialDomainActionInput{Action: "set_default", ExpectedVersion: primary.Version}, adminaccess.MutationAuthority{Reason: "set reviewed default host", CorrelationID: "p17-t017-default", IdempotencyKey: "p17-t017-default-key"}, now.Add(5*time.Second))
	if err != nil {
		return out, err
	}
	_, _, defaultDisableErr := service.MutateOfficialDomain(ctx, root, primary.ID, adminaccess.OfficialDomainActionInput{Action: "disable", ExpectedVersion: primary.Version}, adminaccess.MutationAuthority{Reason: "default host disable conflict probe", CorrelationID: "p17-t017-default-disable", IdempotencyKey: "p17-t017-default-disable-key"}, now.Add(6*time.Second))
	secondary, _, err := service.CreateOfficialDomain(ctx, root, adminaccess.CreateOfficialDomainInput{Hostname: "www.gojet.cc"}, adminaccess.MutationAuthority{Reason: "register reviewed secondary official host", CorrelationID: "p17-t017-secondary-create", IdempotencyKey: "p17-t017-secondary-create-key"}, now.Add(7*time.Second))
	if err != nil {
		return out, err
	}
	secondary, _, err = service.MutateOfficialDomain(ctx, root, secondary.ID, adminaccess.OfficialDomainActionInput{Action: "disable", ExpectedVersion: secondary.Version}, adminaccess.MutationAuthority{Reason: "disable reviewed non-default host", CorrelationID: "p17-t017-secondary-disable", IdempotencyKey: "p17-t017-secondary-disable-key"}, now.Add(8*time.Second))
	if err != nil {
		return out, err
	}
	_, _, deniedErr := service.CreateOfficialDomain(ctx, other, adminaccess.CreateOfficialDomainInput{Hostname: "alt.gojet.cc"}, adminaccess.MutationAuthority{Reason: "permission boundary probe", CorrelationID: "p17-t017-denied", IdempotencyKey: "p17-t017-denied-key"}, now.Add(9*time.Second))
	var riskAfter string
	if err := runtime.DB.QueryRowContext(ctx, `SELECT risk_fingerprint FROM links WHERE code='official-risk-probe'`).Scan(&riskAfter); err != nil {
		return out, err
	}
	var p16After int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM destination_risk_decisions`).Scan(&p16After); err != nil {
		return out, err
	}
	var auditRows int
	if err := runtime.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM admin_audit_events WHERE resource_type='official_domain'`).Scan(&auditRows); err != nil {
		return out, err
	}
	out.RecordCounts["official_domains"] = 2
	out.RecordCounts["audit_events"] = auditRows
	out.Checks["domains_manage_required"] = errors.Is(deniedErr, adminaccess.ErrForbidden)
	out.Checks["default_disable_conflict_preserved"] = errors.Is(defaultDisableErr, adminaccess.ErrConflict) && primary.Enabled && primary.IsDefault && primary.HTTPSState == "active" && primary.Version == 3
	out.Checks["non_default_disable_lifecycle"] = !secondary.Enabled && !secondary.IsDefault && secondary.HTTPSState == "pending" && secondary.Version == 2
	out.Checks["p05_risk_fingerprint_unchanged"] = riskBefore == riskAfter && riskAfter != ""
	out.Checks["p16_destination_risk_authority_untouched"] = p16Before == p16After
	out.Checks["lifecycle_audited"] = auditRows == 5
	pass(&out)
	return out, nil
}
