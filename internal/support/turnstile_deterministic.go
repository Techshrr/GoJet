package support

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// DeterministicTurnstileVerifier is CI/test-only server-side authority. It stores
// only the expected token digest and never provides a production bypass path.
type DeterministicTurnstileVerifier struct {
	expectedDigest [32]byte
}

func NewDeterministicTurnstileVerifier(expectedToken string) (*DeterministicTurnstileVerifier, error) {
	expectedToken = strings.TrimSpace(expectedToken)
	if expectedToken == "" {
		return nil, ErrInvalidInput
	}
	return &DeterministicTurnstileVerifier{expectedDigest: sha256.Sum256([]byte(expectedToken))}, nil
}

func (v *DeterministicTurnstileVerifier) Verify(_ context.Context, token string) (TurnstileVerification, error) {
	if v == nil || v.expectedDigest == ([32]byte{}) {
		return TurnstileVerification{}, ErrTurnstileRejected
	}
	digest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return TurnstileVerification{Success: subtle.ConstantTimeCompare(digest[:], v.expectedDigest[:]) == 1}, nil
}
