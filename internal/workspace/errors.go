package workspace

import "errors"

var (
	ErrNotFound      = errors.New("workspace resource not found")
	ErrForbidden     = errors.New("workspace access denied")
	ErrConflict      = errors.New("workspace version conflict")
	ErrLastOwner     = errors.New("last workspace owner is protected")
	ErrInvalid       = errors.New("invalid workspace input")
	ErrInUse         = errors.New("workspace resource is in use")
	ErrInviteExpired = errors.New("workspace invitation expired")
	ErrInviteState   = errors.New("workspace invitation is not pending")
	ErrAccountMatch  = errors.New("workspace invitation account mismatch")
)
