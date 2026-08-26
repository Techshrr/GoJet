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
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
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
	grantKey, err := runnerutil.GrantKey()
	if err != nil {
		fail(err)
	}

	stamp := time.Now().UTC().UnixNano()
	now := time.Now().UTC()
	registration, _ := auth.NewRegistrationService(db, grantKey)
	verification, _ := auth.NewVerificationService(db)
	passwords, _ := auth.NewPasswordService(db)
	oldPassword := "P15-T014 Old Password!"
	newPassword := "P15-T014 New Password!"
	created, err := registration.Register(ctx, auth.RegistrationInput{Email: fmt.Sprintf("p15-t014-%d@example.test", stamp), DisplayName: "P15 T014", CorrelationID: fmt.Sprintf("p15-t014-register-%d", stamp)})
	if err != nil {
		fail(err)
	}
	if err := passwords.SetInitialPassword(ctx, created.User.ID, oldPassword, fmt.Sprintf("p15-t014-initial-%d", stamp)); err != nil {
		fail(err)
	}
	verified, err := verification.VerifyEmail(ctx, auth.EmailVerificationInput{Code: created.VerificationCode, CorrelationID: fmt.Sprintf("p15-t014-verify-%d", stamp)})
	if err != nil {
		fail(err)
	}
	currentSecret, err := runnerutil.CreateSession(ctx, db, verified.User.ID, fmt.Sprintf("p15-t014-current-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	secondary, err := runnerutil.CreateSession(ctx, db, verified.User.ID, fmt.Sprintf("p15-t014-secondary-%d", stamp), time.Hour)
	if err != nil {
		fail(err)
	}
	accounts, _ := auth.NewAccountService(db)

	_, badOriginErr := runnerutil.AuthorizeMutationRequest(ctx, redisClient, currentSecret.Session, http.MethodPatch, "https://evil.example", true, now)
	_, missingCSRFErr := runnerutil.AuthorizeMutationRequest(ctx, redisClient, currentSecret.Session, http.MethodPatch, runnerutil.AllowedOrigin, false, now.Add(time.Millisecond))
	profileAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, currentSecret.Session, http.MethodPatch, runnerutil.AllowedOrigin, now.Add(2*time.Millisecond))
	if err != nil {
		fail(err)
	}
	updated, profileErr := accounts.UpdateProfile(ctx, currentSecret.Session, profileAuthority, "P15 T014 Updated", fmt.Sprintf("p15-t014-profile-%d", stamp), now.Add(2*time.Millisecond))

	passwordAuthority, err := runnerutil.MutationAuthority(ctx, redisClient, currentSecret.Session, http.MethodPatch, runnerutil.AllowedOrigin, now.Add(3*time.Millisecond))
	if err != nil {
		fail(err)
	}
	passwordErr := accounts.ChangePassword(ctx, currentSecret.Session, passwordAuthority, oldPassword, newPassword, fmt.Sprintf("p15-t014-password-%d", stamp), now.Add(3*time.Millisecond))
	_, secondaryErr := auth.NewStore(db).GetSessionByToken(ctx, secondary.Token, time.Now().UTC())
	_, currentErr := auth.NewStore(db).GetSessionByToken(ctx, currentSecret.Token, time.Now().UTC())
	login, _ := auth.NewPasswordLoginService(db, time.Hour)
	_, oldLoginErr := login.LoginPassword(ctx, auth.PasswordLoginInput{Email: verified.User.Email, Password: oldPassword, CorrelationID: fmt.Sprintf("p15-t014-old-login-%d", stamp)})
	newLogin, newLoginErr := login.LoginPassword(ctx, auth.PasswordLoginInput{Email: verified.User.Email, Password: newPassword, CorrelationID: fmt.Sprintf("p15-t014-new-login-%d", stamp)})

	staleProof, err := runnerutil.MutationAuthority(ctx, redisClient, currentSecret.Session, http.MethodPatch, runnerutil.AllowedOrigin, now.Add(4*time.Millisecond))
	if err != nil {
		fail(err)
	}
	if err := auth.NewStore(db).RevokeOwnedSession(ctx, verified.User.ID, currentSecret.Session.ID, now.Add(5*time.Millisecond)); err != nil {
		fail(err)
	}
	_, staleErr := accounts.UpdateProfile(ctx, currentSecret.Session, staleProof, "Must Not Apply", fmt.Sprintf("p15-t014-stale-%d", stamp), now.Add(6*time.Millisecond))
	var displayName string
	if err := db.QueryRowContext(ctx, `SELECT display_name FROM auth_users WHERE id=?`, verified.User.ID).Scan(&displayName); err != nil {
		fail(err)
	}
	audits, err := runnerutil.Count(ctx, db, `SELECT COUNT(*) FROM auth_audit_events WHERE user_id=? AND action IN ('auth.profile.updated','auth.password.changed') AND result='success'`, verified.User.ID)
	if err != nil {
		fail(err)
	}

	checks := map[string]bool{
		"disallowed_origin_rejected_before_mutation":  errors.Is(badOriginErr, auth.ErrForbidden),
		"missing_csrf_rejected_before_mutation":       errors.Is(missingCSRFErr, auth.ErrForbidden),
		"profile_mutation_requires_request_authority": profileErr == nil && updated.DisplayName == "P15 T014 Updated" && displayName == "P15 T014 Updated",
		"password_change_is_server_authoritative":     passwordErr == nil && errors.Is(oldLoginErr, auth.ErrUnauthorized) && newLoginErr == nil && newLogin.Session.UserID == verified.User.ID,
		"password_change_revokes_other_sessions":      errors.Is(secondaryErr, auth.ErrRevoked) && currentErr == nil,
		"stale_session_cannot_mutate_account":         errors.Is(staleErr, auth.ErrRevoked) && displayName == "P15 T014 Updated",
		"security_mutations_are_correlated_audits":    audits == 2,
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: "P15-T014", Status: status, MySQLVersion: mysqlVersion, RecordCounts: map[string]int{"security_mutation_audits": audits}, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}
