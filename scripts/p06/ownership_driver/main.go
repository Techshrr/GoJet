package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Techshrr/GoJet/internal/domains"
	_ "github.com/go-sql-driver/mysql"
)

type caseResult struct {
	CaseID  string         `json:"case_id"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details"`
	Errors  []string       `json:"errors"`
}

func main() {
	caseFlag := flag.String("case", "P06-T009", "P06 ownership case ID")
	flag.Parse()

	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		failFatal("GOJET_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		failFatal(err.Error())
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		failFatal(fmt.Sprintf("ping MySQL: %v", err))
	}

	result := caseResult{CaseID: *caseFlag, Status: "PASS", Details: map[string]any{}, Errors: []string{}}
	switch *caseFlag {
	case "P06-T009":
		if err := caseT009(ctx, db, &result); err != nil {
			result.Status = "FAIL"
			result.Errors = append(result.Errors, err.Error())
		}
	default:
		result.Status = "FAIL"
		result.Errors = append(result.Errors, fmt.Sprintf("unsupported case %s", *caseFlag))
	}
	writeJSON(map[string]caseResult{*caseFlag: result})
	if result.Status != "PASS" {
		os.Exit(1)
	}
}

func caseT009(ctx context.Context, db *sql.DB, out *caseResult) error {
	workspace := "p06-t009-rotation"
	store := domains.NewMySQLStore(db)
	now := fixedNow()
	if _, err := store.UpsertPlanSource(ctx, domains.PlanSourceInput{
		WorkspaceID: workspace,
		SourceKey: "subscription-business-t009",
		Status: domains.EntitlementActive,
		DomainLimit: 1,
		StartsAt: now.Add(-24 * time.Hour),
		DecisionReason: "T009 active entitlement fixture",
	}, "corr-p06-t009-plan"); err != nil {
		return err
	}

	created, err := store.CreateDomain(ctx, domains.CreateDomainInput{
		WorkspaceID: workspace,
		ActorID: "actor-t009",
		CorrelationID: "corr-p06-t009-create",
		Reason: "create domain before ownership rotation",
		Hostname: "rotate-t009.example.com",
		Now: now,
	})
	if err != nil {
		return err
	}
	initialSecret, err := secretFromTXTValue(created.OwnershipTXTValue)
	if err != nil {
		return err
	}
	if created.Domain.OwnershipTokenVersion != 1 || created.Domain.OwnershipStatus != domains.OwnershipPending {
		return fmt.Errorf("unexpected initial ownership state: version=%d status=%s", created.Domain.OwnershipTokenVersion, created.Domain.OwnershipStatus)
	}

	initialVerifier, initialVersion, err := persistedVerifier(ctx, db, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if initialVersion != 1 || !domains.OwnershipSecretMatches(initialSecret, initialVerifier) {
		return errors.New("initial plaintext does not match persisted verifier")
	}
	plaintextColumns, err := scalarInt(ctx, db, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = DATABASE() AND table_name = 'custom_domains'
		AND column_name IN ('ownership_secret', 'ownership_secret_plaintext', 'ownership_txt_value')`)
	if err != nil {
		return err
	}
	if plaintextColumns != 0 {
		return fmt.Errorf("plaintext ownership secret column exists: count=%d", plaintextColumns)
	}
	publicDomainJSON, err := json.Marshal(created.Domain)
	if err != nil {
		return err
	}
	if strings.Contains(string(publicDomainJSON), initialSecret) || strings.Contains(string(publicDomainJSON), "ownership_secret_hash") || strings.Contains(string(publicDomainJSON), "ownership_txt_value") {
		return fmt.Errorf("normal Domain serialization exposed secret material: %s", publicDomainJSON)
	}

	rotatedAt := now.Add(5 * time.Minute)
	rotated, err := store.RotateOwnershipSecret(ctx, domains.RotateOwnershipSecretInput{
		WorkspaceID: workspace,
		DomainID: created.Domain.ID,
		ActorID: "actor-t009",
		CorrelationID: "corr-p06-t009-rotate",
		Reason: "rotate ownership proof",
		Now: rotatedAt,
	})
	if err != nil {
		return err
	}
	rotatedSecret, err := secretFromTXTValue(rotated.OwnershipTXTValue)
	if err != nil {
		return err
	}
	if rotatedSecret == initialSecret {
		return errors.New("ownership rotation reused prior plaintext secret")
	}
	if rotated.Domain.OwnershipTokenVersion != 2 || rotated.Domain.OwnershipStatus != domains.OwnershipPending || rotated.Domain.OwnershipVerifiedAt != nil {
		return fmt.Errorf("rotation did not reset ownership authority: version=%d status=%s verified_at=%v", rotated.Domain.OwnershipTokenVersion, rotated.Domain.OwnershipStatus, rotated.Domain.OwnershipVerifiedAt)
	}
	if !rotated.Domain.OwnershipSecretIssuedAt.Equal(rotatedAt.UTC()) {
		return fmt.Errorf("rotation issued_at=%s want %s", rotated.Domain.OwnershipSecretIssuedAt, rotatedAt.UTC())
	}

	rotatedVerifier, rotatedVersion, err := persistedVerifier(ctx, db, workspace, created.Domain.ID)
	if err != nil {
		return err
	}
	if rotatedVersion != 2 {
		return fmt.Errorf("persisted token version=%d want 2", rotatedVersion)
	}
	if domains.OwnershipSecretMatches(initialSecret, rotatedVerifier) {
		return errors.New("prior ownership secret remained valid after rotation")
	}
	if !domains.OwnershipSecretMatches(rotatedSecret, rotatedVerifier) {
		return errors.New("rotated ownership secret does not match current verifier")
	}

	var auditMetadata string
	if err := db.QueryRowContext(ctx, `
		SELECT CAST(metadata_json AS CHAR)
		FROM custom_domain_audit_events
		WHERE workspace_id = ? AND domain_id = ? AND action = 'domain.ownership.rotate'
		AND result = 'success' AND correlation_id = ?
		ORDER BY id DESC LIMIT 1`, workspace, created.Domain.ID, "corr-p06-t009-rotate").Scan(&auditMetadata); err != nil {
		return err
	}
	if strings.Contains(auditMetadata, initialSecret) || strings.Contains(auditMetadata, rotatedSecret) || strings.Contains(auditMetadata, created.OwnershipTXTValue) || strings.Contains(auditMetadata, rotated.OwnershipTXTValue) {
		return fmt.Errorf("rotation audit leaked ownership secret material: %s", auditMetadata)
	}
	var auditPayload map[string]any
	if err := json.Unmarshal([]byte(auditMetadata), &auditPayload); err != nil {
		return fmt.Errorf("parse rotation audit metadata: %w", err)
	}
	if auditPayload["previous_token_version"] != float64(1) || auditPayload["ownership_token_version"] != float64(2) || auditPayload["ownership_status"] != string(domains.OwnershipPending) {
		return fmt.Errorf("rotation audit missing version/state transition: %v", auditPayload)
	}

	out.Details = map[string]any{
		"domain_id": created.Domain.ID,
		"hostname_ascii": created.Domain.HostnameASCII,
		"initial_token_version": 1,
		"rotated_token_version": 2,
		"persisted_verifier_bytes": 32,
		"plaintext_secret_columns": plaintextColumns,
		"domain_serialization_exposes_secret": false,
		"old_secret_invalidated": true,
		"new_secret_matches_current_verifier": true,
		"ownership_reset_to_pending": true,
		"rotation_audit_secret_leak": false,
	}
	return nil
}

func persistedVerifier(ctx context.Context, db *sql.DB, workspaceID string, domainID uint64) ([32]byte, uint64, error) {
	var raw []byte
	var version uint64
	if err := db.QueryRowContext(ctx, `
		SELECT ownership_secret_hash, ownership_token_version
		FROM custom_domains
		WHERE workspace_id = ? AND id = ?`, workspaceID, domainID).Scan(&raw, &version); err != nil {
		return [32]byte{}, 0, err
	}
	if len(raw) != 32 {
		return [32]byte{}, 0, fmt.Errorf("persisted ownership verifier length=%d want 32", len(raw))
	}
	var verifier [32]byte
	copy(verifier[:], raw)
	return verifier, version, nil
}

func secretFromTXTValue(value string) (string, error) {
	const prefix = "gojet-verification="
	if !strings.HasPrefix(value, prefix) {
		return "", fmt.Errorf("unexpected ownership TXT value contract: %q", value)
	}
	secret := strings.TrimPrefix(value, prefix)
	if secret == "" {
		return "", errors.New("empty ownership TXT secret")
	}
	return secret, nil
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var value int
	if err := db.QueryRowContext(ctx, query, args...).Scan(&value); err != nil {
		return 0, err
	}
	return value, nil
}

func fixedNow() time.Time {
	return time.Date(2026, 8, 21, 16, 0, 0, 0, time.UTC)
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		failFatal(err.Error())
	}
}

func failFatal(message string) {
	_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"status": "FAIL", "error": message})
	os.Exit(2)
}
