package admin

import "errors"

var (
	ErrInvalid        = errors.New("admin: invalid input")
	ErrUnauthorized   = errors.New("admin: unauthorized")
	ErrForbidden      = errors.New("admin: forbidden")
	ErrNotFound       = errors.New("admin: not found")
	ErrConflict       = errors.New("admin: conflict")
	ErrLocked         = errors.New("admin: locked")
	ErrRateLimited    = errors.New("admin: rate limited")
	ErrMFARequired    = errors.New("admin: mfa required")
	ErrMFAInvalid     = errors.New("admin: invalid mfa code")
	ErrReasonRequired = errors.New("admin: reason required")
	ErrReplayMismatch = errors.New("admin: idempotency replay mismatch")
)
