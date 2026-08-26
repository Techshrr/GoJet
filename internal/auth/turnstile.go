package auth

import (
	"context"

	"github.com/Techshrr/GoJet/internal/support"
)

// AuthTurnstileGuard intentionally delegates to the P14 shared verifier and
// replay-store contract. P15 does not create a second verification authority.
type AuthTurnstileGuard struct {
	verifier support.TurnstileVerifier
	replay   support.TurnstileReplayStore
}

func NewAuthTurnstileGuard(verifier support.TurnstileVerifier, replay support.TurnstileReplayStore) (*AuthTurnstileGuard, error) {
	if verifier == nil || replay == nil {
		return nil, ErrInvalid
	}
	return &AuthTurnstileGuard{verifier: verifier, replay: replay}, nil
}

func (g *AuthTurnstileGuard) Verify(ctx context.Context, rawToken string) error {
	if g == nil || g.verifier == nil || g.replay == nil {
		return ErrForbidden
	}
	return support.VerifyProtectedSubmission(ctx, rawToken, g.verifier, g.replay)
}
