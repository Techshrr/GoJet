package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/Techshrr/GoJet/internal/links"
	"github.com/Techshrr/GoJet/internal/trust"
)

type output struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	Fixture      string          `json:"fixture"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

type permissionFixture struct {
	allow bool
	calls []string
}

func (p *permissionFixture) Authorize(_ context.Context, actorID, permission string) error {
	p.calls = append(p.calls, strings.TrimSpace(actorID)+":"+strings.TrimSpace(permission))
	if !p.allow || strings.TrimSpace(actorID) == "" || permission != trust.SecurityManagePermission {
		return trust.ErrUnauthorized
	}
	return nil
}

func main() {
	out, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		out.Case = "P16-T021"
		out.Status = "FAIL"
		if out.Checks == nil {
			out.Checks = map[string]bool{"runner_completed": false}
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	if err != nil || out.Status != "PASS" {
		os.Exit(1)
	}
}

func run() (output, error) {
	out := output{
		Case:         "P16-T021",
		Status:       "FAIL",
		Fixture:      "real MySQL 8.x admin abuse lifecycle proving security.manage, optimistic versioning, idempotent success replay, conflict audit and terminal resolved/dismissed states",
		RecordCounts: map[string]int{},
		Checks:       map[string]bool{},
	}
	dsn := strings.TrimSpace(os.Getenv("GOJET_MYSQL_DSN"))
	if dsn == "" {
		return out, fmt.Errorf("GOJET_MYSQL_DSN is required")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return out, err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return out, err
	}
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&out.MySQLVersion); err != nil {
		return out, err
	}

	reportOne, err := seedOpenReport(ctx, db, "t021-a")
	if err != nil {
		return out, err
	}
	reportTwo, err := seedOpenReport(ctx, db, "t021-b")
	if err != nil {
		return out, err
	}
	store := trust.NewStore(db)
	denied := &permissionFixture{allow: false}
	allowed := &permissionFixture{allow: true}

	_, deniedErr := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportOne,
		ExpectedVersion: 1,
		ToStatus:        trust.AbuseInvestigating,
		Reason:          "begin validated security review",
		ActorID:         "p16-t021-denied",
		CorrelationID:   "p16-t021-denied-correlation",
		IdempotencyKey:  "p16-t021-denied-idem",
	}, denied)

	investigating, err := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportOne,
		ExpectedVersion: 1,
		ToStatus:        trust.AbuseInvestigating,
		Reason:          "begin validated security review",
		ActorID:         "p16-t021-admin",
		CorrelationID:   "p16-t021-investigating",
		IdempotencyKey:  "p16-t021-investigating-idem",
	}, allowed)
	if err != nil {
		return out, err
	}
	replay, err := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportOne,
		ExpectedVersion: 1,
		ToStatus:        trust.AbuseInvestigating,
		Reason:          "begin validated security review",
		ActorID:         "p16-t021-admin",
		CorrelationID:   "p16-t021-replay-correlation",
		IdempotencyKey:  "p16-t021-investigating-idem",
	}, allowed)
	if err != nil {
		return out, err
	}
	_, conflictingReplayErr := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportOne,
		ExpectedVersion: 1,
		ToStatus:        trust.AbuseDismissed,
		Reason:          "different request under same idempotency authority",
		ActorID:         "p16-t021-admin",
		CorrelationID:   "p16-t021-conflicting-replay",
		IdempotencyKey:  "p16-t021-investigating-idem",
	}, allowed)
	_, staleErr := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportOne,
		ExpectedVersion: 1,
		ToStatus:        trust.AbuseResolved,
		Reason:          "stale optimistic version",
		ActorID:         "p16-t021-admin",
		CorrelationID:   "p16-t021-stale",
		IdempotencyKey:  "p16-t021-stale-idem",
	}, allowed)
	resolved, err := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportOne,
		ExpectedVersion: 2,
		ToStatus:        trust.AbuseResolved,
		Reason:          "review completed with resource action recorded",
		ActorID:         "p16-t021-admin",
		CorrelationID:   "p16-t021-resolved",
		IdempotencyKey:  "p16-t021-resolved-idem",
	}, allowed)
	if err != nil {
		return out, err
	}
	_, terminalErr := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportOne,
		ExpectedVersion: 3,
		ToStatus:        trust.AbuseDismissed,
		Reason:          "terminal state cannot be rewritten",
		ActorID:         "p16-t021-admin",
		CorrelationID:   "p16-t021-terminal-conflict",
		IdempotencyKey:  "p16-t021-terminal-idem",
	}, allowed)
	dismissed, err := store.TransitionAbuseReport(ctx, trust.AbuseAdminTransitionInput{
		ReportID:        reportTwo,
		ExpectedVersion: 1,
		ToStatus:        trust.AbuseDismissed,
		Reason:          "report reviewed and dismissed",
		ActorID:         "p16-t021-admin",
		CorrelationID:   "p16-t021-dismissed",
		IdempotencyKey:  "p16-t021-dismissed-idem",
	}, allowed)
	if err != nil {
		return out, err
	}

	successEvents, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_report_events WHERE action='abuse.admin-transition' AND result='success' AND report_id IN (?,?)`, reportOne, reportTwo)
	if err != nil {
		return out, err
	}
	deniedEvents, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_report_events WHERE report_id=? AND action='abuse.admin-transition' AND result='denied' AND reason_category='permission-denied'`, reportOne)
	if err != nil {
		return out, err
	}
	conflictEvents, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_report_events WHERE report_id=? AND action='abuse.admin-transition' AND result='conflict'`, reportOne)
	if err != nil {
		return out, err
	}
	idempotentEvents, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM abuse_report_events WHERE report_id=? AND action='abuse.admin-transition' AND idempotency_key_hash IS NOT NULL`, reportOne)
	if err != nil {
		return out, err
	}
	uniqueIndex, err := scalarInt(ctx, db, `SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema=DATABASE() AND table_name='abuse_report_events' AND index_name='uq_abuse_events_idempotency'`)
	if err != nil {
		return out, err
	}
	storedOne, err := store.GetAbuseReport(ctx, reportOne)
	if err != nil {
		return out, err
	}
	storedTwo, err := store.GetAbuseReport(ctx, reportTwo)
	if err != nil {
		return out, err
	}

	out.RecordCounts = map[string]int{
		"successful_transition_events":             successEvents,
		"denied_transition_events":                 deniedEvents,
		"conflict_transition_events":               conflictEvents,
		"successful_idempotency_events_report_one": idempotentEvents,
		"idempotency_unique_index_entries":         uniqueIndex,
	}
	out.Checks = map[string]bool{
		"security_manage_is_required_and_denial_is_audited":      errors.Is(deniedErr, trust.ErrUnauthorized) && deniedEvents == 1 && len(denied.calls) == 1,
		"open_moves_to_investigating_with_optimistic_version":    investigating.Changed && investigating.Report.Status == trust.AbuseInvestigating && investigating.Report.Version == 2,
		"successful_retry_is_idempotent_without_duplicate_event": !replay.Changed && replay.Report.Status == trust.AbuseInvestigating && replay.Report.Version == 2 && idempotentEvents == 2,
		"same_idempotency_key_with_different_request_conflicts":  errors.Is(conflictingReplayErr, trust.ErrConflict),
		"stale_expected_version_conflicts_and_is_audited":        errors.Is(staleErr, trust.ErrConflict) && conflictEvents >= 2,
		"investigating_moves_to_resolved":                        resolved.Changed && storedOne.Status == trust.AbuseResolved && storedOne.Version == 3,
		"resolved_is_terminal":                                   errors.Is(terminalErr, trust.ErrConflict) && storedOne.Status == trust.AbuseResolved,
		"open_can_be_reasonedly_dismissed":                       dismissed.Changed && storedTwo.Status == trust.AbuseDismissed && storedTwo.Version == 2,
		"success_history_is_single_row_per_state_change":         successEvents == 3,
		"database_enforces_success_idempotency_uniqueness":       uniqueIndex >= 3,
	}
	if allTrue(out.Checks) {
		out.Status = "PASS"
	}
	return out, nil
}

func seedOpenReport(ctx context.Context, db *sql.DB, suffix string) (uint64, error) {
	primary := "https://safe.example/" + suffix
	fingerprint, _, err := links.RiskFingerprint(primary, nil, nil)
	if err != nil {
		return 0, err
	}
	linkResult, err := db.ExecContext(ctx, `
INSERT INTO links
(workspace_id,hostname,domain_kind,code,title,primary_destination,redirect_status,status,version,risk_fingerprint,routing_json,ab_json,utm_json,access_json)
VALUES (?,?,'official',?,?,?,302,'active',1,?,'[]','[]','{}','{}')`,
		"p16-t021-workspace", "go.example.test", suffix, "P16 T021", primary, fingerprint)
	if err != nil {
		return 0, err
	}
	linkID, err := linkResult.LastInsertId()
	if err != nil {
		return 0, err
	}
	publicID := "abr_" + suffix
	result, err := db.ExecContext(ctx, `
INSERT INTO abuse_reports
(public_id,workspace_id,resource_type,resource_id,hostname_ascii,safe_code,destination_fingerprint,category,details_redacted,request_fingerprint,idempotency_key_hash,status,version,correlation_id,evidence_ref)
VALUES (?,?,'short-link-risk',?,?,?,?,?,'fixture report',?,?,'open',1,?,?)`,
		publicID, "p16-t021-workspace", fmt.Sprintf("%d", linkID), "go.example.test", suffix, fingerprint, "phishing", hash64("request-"+suffix), hash64("idem-"+suffix), "p16-t021-seed-"+suffix, "abuse-report:"+publicID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	return uint64(id), err
}

func hash64(value string) string {
	fingerprint, _, _ := links.RiskFingerprint("https://hash.example/"+value, nil, nil)
	return fingerprint
}

func scalarInt(ctx context.Context, db *sql.DB, query string, args ...any) (int, error) {
	var n int
	err := db.QueryRowContext(ctx, query, args...).Scan(&n)
	return n, err
}

func allTrue(checks map[string]bool) bool {
	if len(checks) == 0 {
		return false
	}
	for _, ok := range checks {
		if !ok {
			return false
		}
	}
	return true
}
