package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	authn "github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/internal/trust"
	"github.com/Techshrr/GoJet/scripts/p16/adminfixture"
	"github.com/Techshrr/GoJet/scripts/p16/domainfixture"
	"github.com/Techshrr/GoJet/scripts/p16/runtimefixture"
)

const (
	securityActor = "p16-browser-security-admin"
	domainActor   = "p16-browser-domain-admin"
	deniedActor   = "p16-browser-denied-admin"
)

type sessionOutput struct {
	CookieName      string `json:"cookie_name"`
	SecurityActor   string `json:"security_actor"`
	SecuritySession string `json:"security_session"`
	DomainActor     string `json:"domain_actor"`
	DomainSession   string `json:"domain_session"`
	DeniedActor     string `json:"denied_actor"`
	DeniedSession   string `json:"denied_session"`
}

type seedOutput struct {
	DestinationRiskID uint64 `json:"destination_risk_id"`
	DestinationLinkID uint64 `json:"destination_link_id"`
	DestinationHost   string `json:"destination_host"`
	DestinationCode   string `json:"destination_code"`
	DestinationFP     string `json:"destination_fingerprint"`
	DomainID          uint64 `json:"domain_id"`
	DomainHost        string `json:"domain_host"`
	AbuseID           uint64 `json:"abuse_id"`
	AbusePublicID     string `json:"abuse_public_id"`
	SensitiveTarget   string `json:"sensitive_target"`
	ProviderMarker    string `json:"provider_marker"`
}

type publicSeedOutput struct {
	LinkID      uint64 `json:"link_id"`
	WorkspaceID string `json:"workspace_id"`
	Hostname    string `json:"hostname"`
	Code        string `json:"code"`
	Fingerprint string `json:"fingerprint"`
}

func main() {
	mode := flag.String("mode", "sessions", "sessions, seed-admin or seed-public")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	db, err := domainfixture.OpenDB()
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	var value any
	switch strings.TrimSpace(*mode) {
	case "sessions":
		value, err = createSessions(ctx, db)
	case "seed-admin":
		value, err = seedAdmin(ctx, db)
	case "seed-public":
		value, err = seedPublic(ctx, db)
	default:
		err = fmt.Errorf("unsupported browser fixture mode %q", *mode)
	}
	if err != nil {
		fatal(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		fatal(err)
	}
}

func createSessions(ctx context.Context, db *sql.DB) (sessionOutput, error) {
	security, err := adminfixture.EnsureSession(ctx, db, securityActor)
	if err != nil {
		return sessionOutput{}, err
	}
	domain, err := adminfixture.EnsureSession(ctx, db, domainActor)
	if err != nil {
		return sessionOutput{}, err
	}
	denied, err := adminfixture.EnsureSession(ctx, db, deniedActor)
	if err != nil {
		return sessionOutput{}, err
	}
	return sessionOutput{
		CookieName:      authn.SessionCookieName,
		SecurityActor:   securityActor,
		SecuritySession: security,
		DomainActor:     domainActor,
		DomainSession:   domain,
		DeniedActor:     deniedActor,
		DeniedSession:   denied,
	}, nil
}

func seedAdmin(ctx context.Context, db *sql.DB) (seedOutput, error) {
	suffix := uniqueSuffix()
	workspace := "p16-browser-dest-" + suffix
	code := "risk-" + suffix
	const (
		sensitiveTarget = "https://customer.example/p16-browser-sensitive-target"
		providerMarker  = "p16-browser-provider-secret-marker"
		destPolicy      = "p16-browser-destination-v1"
		domainPolicy    = "p16-browser-domain-v1"
	)
	link, err := runtimefixture.CreateLink(ctx, db, workspace, "go.p16.example.test", "official", code, sensitiveTarget, nil, nil)
	if err != nil {
		return seedOutput{}, err
	}
	store := trust.NewStore(db)
	decision, err := runtimefixture.InsertRawDecision(ctx, db, store, link, destPolicy, "browser-"+suffix, trust.DecisionReview, "provider-partial", nil)
	if err != nil {
		return seedOutput{}, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := db.ExecContext(ctx, `
INSERT INTO destination_risk_provider_observations
(scan_id,provider,outcome,signal_code,evidence_json,observed_at,created_at)
VALUES (?,'semantic-browser','review','provider-partial',?,?,?)`,
		decision.ScanID,
		`{"authorization":"Bearer `+providerMarker+`","target":"`+sensitiveTarget+`"}`,
		now,
		now,
	); err != nil {
		return seedOutput{}, err
	}

	seedNow := now.Add(-2 * time.Minute)
	domain, err := domainfixture.CreateReadyDomain(ctx, db, "browser"+suffix, seedNow)
	if err != nil {
		return seedOutput{}, err
	}
	domainService, err := trust.NewDomainRiskService(store, trust.DomainRiskPolicy{
		Version:           domainPolicy,
		RequiredProviders: []string{"domain-reputation"},
		AllowTTL:          15 * time.Minute,
		RevalidateAfter:   time.Minute,
		RetryAfter:        30 * time.Second,
	}, domainfixture.Provider{
		Name:       "domain-reputation",
		Outcome:    trust.ProviderAllow,
		SignalCode: "domain-allow",
		Evidence:   map[string]any{"fixture": "p16-browser"},
	})
	if err != nil {
		return seedOutput{}, err
	}
	if _, err := domainService.Evaluate(ctx, trust.EvaluateDomainRiskInput{
		WorkspaceID:    domain.WorkspaceID,
		DomainID:       domain.ID,
		RequestKind:    trust.DomainRiskInitial,
		IdempotencyKey: "p16-browser-domain-" + suffix,
		ActorID:        "p16-browser-seed-worker",
		Reason:         "browser fixture initial domain reputation authority",
		CorrelationID:  "p16-browser-domain-seed-" + suffix,
		Now:            seedNow,
	}); err != nil {
		return seedOutput{}, err
	}

	publicID := "abr_browser_" + suffix
	requestFP := digest("request:" + suffix)
	idempotencyHash := digest("idempotency:" + suffix)
	result, err := db.ExecContext(ctx, `
INSERT INTO abuse_reports
(public_id,workspace_id,resource_type,resource_id,hostname_ascii,safe_code,destination_fingerprint,category,details_redacted,
 request_fingerprint,idempotency_key_hash,status,version,correlation_id,evidence_ref,created_at,updated_at)
VALUES (?,?, 'short-link-risk', ?, ?, ?, ?, 'phishing', ?, ?, ?, 'open', 1, ?, ?, ?, ?)`,
		publicID,
		workspace,
		fmt.Sprintf("%d", link.ID),
		link.Hostname,
		link.Code,
		link.Fingerprint,
		"Sanitized browser fixture report. [redacted-url]",
		requestFP,
		idempotencyHash,
		"p16-browser-abuse-"+suffix,
		"abuse-report:"+publicID,
		now,
		now,
	)
	if err != nil {
		return seedOutput{}, err
	}
	abuseID, err := result.LastInsertId()
	if err != nil || abuseID <= 0 {
		if err == nil {
			err = fmt.Errorf("invalid abuse fixture id")
		}
		return seedOutput{}, err
	}

	return seedOutput{
		DestinationRiskID: decision.ScanID,
		DestinationLinkID: link.ID,
		DestinationHost:   link.Hostname,
		DestinationCode:   link.Code,
		DestinationFP:     link.Fingerprint,
		DomainID:          domain.ID,
		DomainHost:        domain.Hostname,
		AbuseID:           uint64(abuseID),
		AbusePublicID:     publicID,
		SensitiveTarget:   sensitiveTarget,
		ProviderMarker:    providerMarker,
	}, nil
}

func seedPublic(ctx context.Context, db *sql.DB) (publicSeedOutput, error) {
	suffix := uniqueSuffix()
	link, err := runtimefixture.CreateLink(
		ctx,
		db,
		"p16-browser-public-"+suffix,
		"go.p16.example.test",
		"official",
		"public-"+suffix,
		"https://customer.example/p16-public-sensitive-target",
		nil,
		nil,
	)
	if err != nil {
		return publicSeedOutput{}, err
	}
	return publicSeedOutput{
		LinkID:      link.ID,
		WorkspaceID: link.WorkspaceID,
		Hostname:    link.Hostname,
		Code:        link.Code,
		Fingerprint: link.Fingerprint,
	}, nil
}

func uniqueSuffix() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
