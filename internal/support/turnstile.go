package support

import (
	"context"
	"crypto/sha256"
	"strings"
)

type TurnstileVerification struct {
	Success bool
}

type TurnstileVerifier interface {
	Verify(ctx context.Context, token string) (TurnstileVerification, error)
}

type TurnstileReplayStore interface {
	ClaimDigest(ctx context.Context, digest [32]byte) (bool, error)
}

func TurnstileTokenDigest(token string) ([32]byte, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return [32]byte{}, ErrInvalidInput
	}
	return sha256.Sum256([]byte(token)), nil
}

// VerifyProtectedSubmission passes the raw token only to the verification provider. The replay
// authority receives a SHA-256 digest and therefore has no interface by which to persist the raw token.
func VerifyProtectedSubmission(ctx context.Context, rawToken string, verifier TurnstileVerifier, replay TurnstileReplayStore) error {
	if verifier == nil || replay == nil {
		return ErrInvalidInput
	}
	rawToken = strings.TrimSpace(rawToken)
	digest, err := TurnstileTokenDigest(rawToken)
	if err != nil {
		return err
	}
	result, err := verifier.Verify(ctx, rawToken)
	if err != nil || !result.Success {
		return ErrTurnstileRejected
	}
	claimed, err := replay.ClaimDigest(ctx, digest)
	if err != nil {
		return err
	}
	if !claimed {
		return ErrTurnstileReplay
	}
	return nil
}
