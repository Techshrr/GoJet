package auth

import "errors"

var (
	ErrInvalid              = errors.New("invalid authentication input")
	ErrNotFound             = errors.New("authentication resource not found")
	ErrConflict             = errors.New("authentication resource conflict")
	ErrUnauthorized         = errors.New("authentication required")
	ErrForbidden            = errors.New("authentication action forbidden")
	ErrVerificationRequired = errors.New("email verification required")
	ErrLocked               = errors.New("authentication account locked")
	ErrRateLimited          = errors.New("authentication request rate limited")
	ErrExpired              = errors.New("authentication authority expired")
	ErrRevoked              = errors.New("authentication authority revoked")
	ErrReplay               = errors.New("authentication authority already consumed")
)
