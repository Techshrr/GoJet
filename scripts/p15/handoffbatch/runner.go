package handoffbatch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
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

type oauthFixture struct {
	service *auth.OAuthService
	config  auth.OAuthProviderConfig
	secret  string
}

func Run(caseID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	db, mysqlVersion, err := runnerutil.OpenMySQL(ctx)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	var checks map[string]bool
	var counts map[string]int
	switch caseID {
	case "P15-T019":
		checks, counts, err = runT019(ctx, db)
	case "P15-T020":
		checks, counts, err = runT020(ctx, db)
	case "P15-T021":
		checks, counts, err = runT021(ctx, db)
	case "P15-T022":
		checks, counts, err = runT022(ctx, db)
	case "P15-T023":
		checks, counts, err = runT023(ctx, db)
	case "P15-T027":
		checks, counts, err = runT027(ctx, db)
	default:
		err = fmt.Errorf("unsupported case %s", caseID)
	}
	if err != nil {
		fail(err)
	}
	status := "PASS"
	for _, ok := range checks {
		if !ok {
			status = "FAIL"
			break
		}
	}
	out := result{Case: caseID, Status: status, MySQLVersion: mysqlVersion, RecordCounts: counts, Checks: checks}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fail(err)
	}
	if status != "PASS" {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}

func newOAuthFixture(ctx context.Context, db *sql.DB, provider, prefix string, now time.Time) (oauthFixture, func(), error) {
	redisClient, err := runnerutil.OpenRedis(ctx)
	if err != nil {
		return oauthFixture{}, nil, err
	}
	cleanup := func() { _ = redisClient.Close() }
	crypto, err := runnerutil.OAuthCrypto()
	if err != nil {
		cleanup()
		return oauthFixture{}, nil, err
	}
	service, err := auth.NewOAuthService(db, crypto, 5*time.Minute)
	if err != nil {
		cleanup()
		return oauthFixture{}, nil, err
	}
	stamp := time.Now().UTC().UnixNano()
	admin, err := runnerutil.ActivateUser(ctx, db, fmt.Sprintf("%s-admin-%d@example.test", prefix, stamp), prefix+" Admin", now)
	if err != nil {
		cleanup()
		return oauthFixture{}, nil, err
	}
	session, err := runnerutil.CreateSession(ctx, db, admin.ID, prefix+"-admin-session", time.Hour)
	if err != nil {
		cleanup()
		return oauthFixture{}, nil, err
	}
	authority, err := runnerutil.MutationAuthority(ctx, redisClient, session.Session, http.MethodPatch, runnerutil.AllowedOrigin, now)
	if err != nil {
		cleanup()
		return oauthFixture{}, nil, err
	}
	permission := &runnerutil.PermissionRecorder{Allowed: true}
	secret := prefix + "-client-secret"
	cfg, err := service.UpdateProviderConfig(ctx, session.Session, authority, permission, admin.ID, prefix+"-config", auth.OAuthProviderUpdate{
		Provider:         provider,
		Enabled:          true,
		ClientID:         prefix + "-client-id",
		ClientSecret:     secret,
		AuthorizationURL: "https://provider.example/authorize",
		TokenURL:         "https://provider.example/token",
		UserInfoURL:      "https://provider.example/userinfo",
		RedirectURI:      "https://gojet.example/oauth/" + provider + "/callback",
		Scopes:           []string{"openid", "email"},
	}, now)
	if err != nil {
		cleanup()
		return oauthFixture{}, nil, err
	}
	return oauthFixture{service: service, config: cfg, secret: secret}, cleanup, nil
}

func callback(ctx context.Context, fx oauthFixture, provider, intent, subject, email string, verified bool, prefix string, now time.Time) (auth.OAuthCallbackResult, error) {
	start, err := fx.service.Start(ctx, auth.OAuthStartInput{Provider: provider, Intent: intent, CorrelationID: prefix + "-start"}, now)
	if err != nil {
		return auth.OAuthCallbackResult{}, err
	}
	adapter := &runnerutil.DeterministicOAuthAdapter{
		ExpectedProvider:     provider,
		ExpectedCode:         prefix + "-provider-code",
		ExpectedClientID:     fx.config.ClientID,
		ExpectedClientSecret: fx.secret,
		ExpectedRedirectURI:  fx.config.RedirectURI,
		ExpectedPKCEVerifier: start.PKCEVerifier,
		Claim: auth.OAuthProviderClaim{
			Subject:       subject,
			Email:         email,
			EmailVerified: verified,
			DisplayName:   prefix + " User",
		},
	}
	return fx.service.Callback(ctx, adapter, auth.OAuthCallbackInput{Provider: provider, State: start.State, Code: prefix + "-provider-code", CorrelationID: prefix + "-callback"}, now.Add(time.Millisecond))
}

func socialCode(ctx context.Context, fx oauthFixture, provider, subject, email string, verified bool, prefix string, now time.Time) (string, error) {
	cb, err := callback(ctx, fx, provider, auth.OAuthIntentRegister, subject, email, verified, prefix, now)
	if err != nil {
		return "", err
	}
	handoff, err := fx.service.CreateBrowserHandoff(ctx, cb, prefix+"-handoff", now.Add(2*time.Millisecond))
	if err != nil {
		return "", err
	}
	exchanged, err := fx.service.ExchangeBrowserHandoff(ctx, handoff.Code, prefix+"-exchange", now.Add(3*time.Millisecond))
	if err != nil {
		return "", err
	}
	if exchanged.Session != nil || !strings.HasPrefix(exchanged.SocialRegistrationCode, "gsr_") {
		return "", auth.ErrForbidden
	}
	return exchanged.SocialRegistrationCode, nil
}

func hashProviderSubject(provider, subject string) []byte {
	h := auth.HashOpaque(provider + "\x00" + strings.TrimSpace(subject))
	return h[:]
}
