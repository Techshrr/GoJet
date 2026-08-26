package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/Techshrr/GoJet/internal/auth"
	"github.com/Techshrr/GoJet/scripts/p15/runnerutil"
)

type result struct {
	Case         string          `json:"case"`
	Status       string          `json:"status"`
	MySQLVersion string          `json:"mysql_version"`
	RecordCounts map[string]int  `json:"record_counts"`
	Checks       map[string]bool `json:"checks"`
}

func fail(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(2) }

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	db, mysqlVersion, err := runnerutil.OpenMySQL(ctx)
	if err != nil {
		fail(err)
	}
	defer db.Close()
	redisClient, err := runnerutil.OpenRedis(ctx)
	if err != nil {
		fail(err)
	}
	defer redisClient.Close()

	stamp := time.Now().UTC().UnixNano()
	now := time.Now().UTC()
	user, err := runnerutil.ActivateUser(ctx, db, fmt.Sprintf("p15-t013-%d@example.test", stamp), "P15 T013", now)
	if err != nil {
		fail(err)
	}
	foreign, err := runnerutil.ActivateUser(ctx, db, fmt.Sprintf("p15-t013-foreign-%d@example.test", stamp), "P15 T013 Foreign", now)
	if err != nil {
		fail(err)
	}
	currentSecret, err := runnerutil.CreateSession(ctx, db, user.ID, fmt.Sprintf("p15-t013-current-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	otherSecret, err := runnerutil.CreateSession(ctx, db, user.ID, fmt.Sprintf("p15-t013-other-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	foreignSecret, err := runnerutil.CreateSession(ctx, db, foreign.ID, fmt.Sprintf("p15-t013-foreign-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}

	accounts, _ := auth.NewAccountService(db)
	listed, listErr := accounts.ListSessions(ctx, currentSecret.Session, now)
	authority, err := runnerutil.MutationAuthority(ctx, redisClient, currentSecret.Session, http.MethodDelete, runnerutil.AllowedOrigin, now)
	if err != nil {
		fail(err)
	}
	revokeErr := accounts.RevokeSession(ctx, currentSecret.Session, authority, otherSecret.Session.ID, fmt.Sprintf("p15-t013-revoke-%d", stamp), now)
	_, revokedLookupErr := auth.NewStore(db).GetSessionByToken(ctx, otherSecret.Token, time.Now().UTC())

	foreignAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, currentSecret.Session, http.MethodDelete, runnerutil.AllowedOrigin, now.Add(time.Second))
	if err != nil {
		fail(err)
	}
	foreignRevokeErr := accounts.RevokeSession(ctx, currentSecret.Session, foreignAuthority, foreignSecret.Session.ID, fmt.Sprintf("p15-t013-foreign-deny-%d", stamp), now.Add(time.Second))
	_, foreignStillActiveErr := auth.NewStore(db).GetSessionByToken(ctx, foreignSecret.Token, time.Now().UTC())

	replayAuthorityErr := accounts.RevokeSession(ctx, currentSecret.Session, authority, currentSecret.Session.ID, fmt.Sprintf("p15-t013-proof-replay-%d", stamp), now.Add(2*time.Second))
	currentAfter, currentErr := auth.NewStore(db).GetSessionByToken(ctx, currentSecret.Token, time.Now().UTC())
	audits, err := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action='auth.session.revoked' AND result='success'`, user.ID)
	if err != nil {
		fail(err)
	}

	checks := map[string]bool{
		"session_list_is_owned_and_safe":         listErr == nil && len(listed) == 2 && allOwnedSummaries(listed, currentSecret.Session.ID, otherSecret.Session.ID),
		"owned_session_revoke_succeeds":          revokeErr == nil,
		"revoked_session_rejected_server_side":   errors.Is(revokedLookupErr, auth.ErrRevoked),
		"foreign_session_id_fails_closed":        errors.Is(foreignRevokeErr, auth.ErrNotFound) && foreignStillActiveErr == nil,
		"mutation_authority_is_one_request_only": errors.Is(replayAuthorityErr, auth.ErrInvalid) && currentErr == nil && currentAfter.ID == currentSecret.Session.ID,
		"session_revoke_is_audited":              audits == 1,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T013", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"listed_sessions": len(listed), "revoke_audits": audits}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func allOwnedSummaries(items []auth.SessionSummary, currentID, otherID string) bool {
	seen := map[string]bool{}
	for _, item := range items {
		if item.ID == "" || item.Status == "" || item.ExpiresAt.IsZero() || item.CreatedAt.IsZero() {
			return false
		}
		seen[item.ID] = true
		if item.Current != (item.ID == currentID) {
			return false
		}
	}
	return seen[currentID] && seen[otherID]
}
