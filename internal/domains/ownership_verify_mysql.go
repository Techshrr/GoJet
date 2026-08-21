package domains

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"time"
)

const ownershipTXTPolicyVersion = "ownership-txt-v1"

type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

type NetTXTResolver struct {
	Resolver *net.Resolver
}

func (r NetTXTResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	resolver := r.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return resolver.LookupTXT(ctx, name)
}

func DefaultTXTResolver() TXTResolver {
	return NetTXTResolver{Resolver: net.DefaultResolver}
}

type OwnershipVerificationOutcome string

const (
	OwnershipVerificationVerified OwnershipVerificationOutcome = "verified"
	OwnershipVerificationMissing  OwnershipVerificationOutcome = "missing"
	OwnershipVerificationMismatch OwnershipVerificationOutcome = "mismatch"
)

type VerifyOwnershipTXTInput struct {
	WorkspaceID   string
	DomainID      uint64
	ActorID       string
	CorrelationID string
	Reason        string
	Now           time.Time
}

type OwnershipVerificationResult struct {
	Domain          Domain                       `json:"domain"`
	Outcome         OwnershipVerificationOutcome `json:"outcome"`
	RecordsObserved int                          `json:"records_observed"`
}

type OwnershipVerifier struct {
	store    *MySQLStore
	resolver TXTResolver
}

func NewOwnershipVerifier(store *MySQLStore, resolver TXTResolver) *OwnershipVerifier {
	if resolver == nil {
		resolver = DefaultTXTResolver()
	}
	return &OwnershipVerifier{store: store, resolver: resolver}
}

// VerifyTXT performs a real resolver lookup before entering the mutation
// transaction. The transaction then row-locks the domain and re-reads the
// current verifier and entitlement so a concurrent secret rotation cannot make
// an old DNS response authoritative.
func (v *OwnershipVerifier) VerifyTXT(ctx context.Context, input VerifyOwnershipTXTInput) (OwnershipVerificationResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.ActorID = strings.TrimSpace(input.ActorID)
	input.CorrelationID = strings.TrimSpace(input.CorrelationID)
	input.Reason = strings.TrimSpace(input.Reason)
	if v == nil || v.store == nil || v.resolver == nil || input.WorkspaceID == "" || input.DomainID == 0 || input.ActorID == "" || input.CorrelationID == "" || input.Reason == "" || input.Now.IsZero() {
		return OwnershipVerificationResult{}, ErrInvalidDomainMutation
	}
	now := input.Now.UTC()

	snapshot, err := v.store.GetDomain(ctx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return OwnershipVerificationResult{}, err
	}
	txtName := OwnershipTXTName(snapshot.HostnameASCII)
	records, lookupErr := v.resolver.LookupTXT(ctx, txtName)
	if lookupErr != nil && !isTXTNotFound(lookupErr) {
		if err := v.recordLookupFailure(ctx, input, snapshot, now); err != nil {
			return OwnershipVerificationResult{}, err
		}
		return OwnershipVerificationResult{}, ErrOwnershipDNSLookup
	}
	if isTXTNotFound(lookupErr) {
		records = nil
	}

	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return OwnershipVerificationResult{}, err
	}
	defer tx.Rollback()

	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return OwnershipVerificationResult{}, err
	}
	entitlement, err := v.store.resolveEntitlementTx(ctx, tx, input.WorkspaceID, now)
	if err != nil {
		return OwnershipVerificationResult{}, err
	}
	if !entitlement.MutationAllowed {
		if auditErr := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.ownership.verify", "denied", entitlement.DecisionReason, input.CorrelationID, map[string]any{
			"code":                    "entitlement_required",
			"ownership_token_version": domain.OwnershipTokenVersion,
		}); auditErr != nil {
			return OwnershipVerificationResult{}, auditErr
		}
		if commitErr := tx.Commit(); commitErr != nil {
			return OwnershipVerificationResult{}, commitErr
		}
		return OwnershipVerificationResult{}, ErrEntitlementRequired
	}
	verifier, err := loadOwnershipVerifierTx(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return OwnershipVerificationResult{}, err
	}

	outcome, status := classifyOwnershipTXT(records, verifier)
	var verifiedAt any
	if status == OwnershipVerified {
		verifiedAt = now
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE custom_domains
		SET ownership_status = ?, ownership_verified_at = ?
		WHERE workspace_id = ? AND id = ?`, status, verifiedAt, input.WorkspaceID, input.DomainID); err != nil {
		return OwnershipVerificationResult{}, err
	}
	updated, err := loadDomainByID(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return OwnershipVerificationResult{}, err
	}

	revalidationResult := "pending"
	auditResult := "denied"
	auditCode := "ownership_proof_missing"
	switch outcome {
	case OwnershipVerificationVerified:
		revalidationResult = "pass"
		auditResult = "success"
		auditCode = "ownership_verified"
	case OwnershipVerificationMismatch:
		revalidationResult = "fail"
		auditCode = "ownership_proof_mismatch"
	}
	metadata := map[string]any{
		"outcome":                 outcome,
		"records_observed":        len(records),
		"ownership_token_version": updated.OwnershipTokenVersion,
	}
	if err := appendOwnershipRevalidationTx(ctx, tx, updated, revalidationResult, now, input.CorrelationID, metadata); err != nil {
		return OwnershipVerificationResult{}, err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &updated.ID, nil, input.ActorID, "domain.ownership.verify", auditResult, input.Reason, input.CorrelationID, map[string]any{
		"code":                    auditCode,
		"outcome":                 outcome,
		"records_observed":        len(records),
		"ownership_token_version": updated.OwnershipTokenVersion,
	}); err != nil {
		return OwnershipVerificationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OwnershipVerificationResult{}, err
	}

	return OwnershipVerificationResult{Domain: updated, Outcome: outcome, RecordsObserved: len(records)}, nil
}

func classifyOwnershipTXT(records []string, verifier [32]byte) (OwnershipVerificationOutcome, OwnershipStatus) {
	sawGoJetProof := false
	for _, record := range records {
		if !strings.HasPrefix(record, "gojet-verification=") {
			continue
		}
		sawGoJetProof = true
		candidate := strings.TrimPrefix(record, "gojet-verification=")
		if candidate != "" && OwnershipSecretMatches(candidate, verifier) {
			return OwnershipVerificationVerified, OwnershipVerified
		}
	}
	if sawGoJetProof {
		return OwnershipVerificationMismatch, OwnershipFailed
	}
	return OwnershipVerificationMissing, OwnershipPending
}

func loadOwnershipVerifierTx(ctx context.Context, tx *sql.Tx, workspaceID string, domainID uint64) ([32]byte, error) {
	var raw []byte
	if err := tx.QueryRowContext(ctx, `
		SELECT ownership_secret_hash
		FROM custom_domains
		WHERE workspace_id = ? AND id = ?`, workspaceID, domainID).Scan(&raw); err != nil {
		return [32]byte{}, err
	}
	if len(raw) != 32 {
		return [32]byte{}, ErrInvalidDomainMutation
	}
	var verifier [32]byte
	copy(verifier[:], raw)
	return verifier, nil
}

func appendOwnershipRevalidationTx(ctx context.Context, tx *sql.Tx, domain Domain, result string, checkedAt time.Time, correlationID string, metadata map[string]any) error {
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO custom_domain_revalidations (
			domain_id, workspace_id, axis, result, policy_version, checked_at,
			next_due_at, evidence_ref, correlation_id, metadata_json
		) VALUES (?, ?, 'ownership', ?, ?, ?, NULL, ?, ?, ?)`,
		domain.ID, domain.WorkspaceID, result, ownershipTXTPolicyVersion, checkedAt,
		"dns:txt:"+OwnershipTXTName(domain.HostnameASCII), correlationID, string(raw),
	)
	return err
}

func (v *OwnershipVerifier) recordLookupFailure(ctx context.Context, input VerifyOwnershipTXTInput, snapshot Domain, checkedAt time.Time) error {
	tx, err := v.store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	domain, err := loadDomainByIDForUpdate(ctx, tx, input.WorkspaceID, input.DomainID)
	if err != nil {
		return err
	}
	if domain.HostnameASCII != snapshot.HostnameASCII {
		return ErrInvalidDomainMutation
	}
	metadata := map[string]any{
		"outcome":                 "lookup_error",
		"records_observed":        0,
		"ownership_token_version": domain.OwnershipTokenVersion,
	}
	if err := appendOwnershipRevalidationTx(ctx, tx, domain, "error", checkedAt, input.CorrelationID, metadata); err != nil {
		return err
	}
	if err := appendDomainAuditTx(ctx, tx, input.WorkspaceID, &domain.ID, nil, input.ActorID, "domain.ownership.verify", "failed", "ownership TXT lookup failed", input.CorrelationID, map[string]any{
		"code":                    "ownership_dns_lookup_failed",
		"records_observed":        0,
		"ownership_token_version": domain.OwnershipTokenVersion,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

func isTXTNotFound(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	return errors.As(err, &dnsErr) && dnsErr.IsNotFound
}
