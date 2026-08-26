package trust

import "errors"

var (
	ErrInvalid          = errors.New("invalid trust input")
	ErrNotFound         = errors.New("trust record not found")
	ErrConflict         = errors.New("trust record conflict")
	ErrStaleFingerprint = errors.New("stale destination risk fingerprint")
	ErrUnauthorized     = errors.New("trust action unauthorized")
	ErrRateLimited      = errors.New("trust action rate limited")
	ErrVerification     = errors.New("trust verification failed")
)
