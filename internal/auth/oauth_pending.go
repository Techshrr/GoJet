package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// PendingState is a read-only dispatch check. Callback still locks and consumes
// the state exactly once before any identity mutation can be committed.
func (s *OAuthService) PendingState(ctx context.Context, provider, state string, now time.Time) (OAuthStartInput, error) {
	if s == nil || s.db == nil || !ValidProvider(provider) || !strings.HasPrefix(state, "gos_") {
		return OAuthStartInput{}, ErrInvalid
	}
	hash := HashOpaque(state)
	var intent string
	var userID, sessionID sql.NullString
	var expires time.Time
	var consumed sql.NullTime
	err := s.db.QueryRowContext(ctx, `SELECT intent,initiating_user_id,initiating_session_id,expires_at,consumed_at FROM oauth_states WHERE provider=? AND state_hash=?`, provider, hash[:]).Scan(&intent, &userID, &sessionID, &expires, &consumed)
	if errors.Is(err, sql.ErrNoRows) {
		return OAuthStartInput{}, ErrForbidden
	}
	if err != nil {
		return OAuthStartInput{}, err
	}
	if consumed.Valid {
		return OAuthStartInput{}, ErrReplay
	}
	if !expires.After(now) {
		return OAuthStartInput{}, ErrExpired
	}
	return OAuthStartInput{Provider: provider, Intent: intent, InitiatingUserID: userID.String, InitiatingSessionID: sessionID.String}, nil
}
