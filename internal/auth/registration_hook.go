package auth

import (
	"context"
	"database/sql"
	"sync"
)

// RegistrationTxHook lets the platform composition layer add required durable
// account-adjacent state to the same registration transaction without moving
// ownership of that state into the auth package.
type RegistrationTxHook func(context.Context, *sql.Tx, User, string) error

var registrationTxHookState struct {
	sync.RWMutex
	hook RegistrationTxHook
}

// ConfigureRegistrationTxHook installs the process-wide platform composition
// hook exactly once. Standalone auth consumers retain the original behavior
// when no hook is configured.
func ConfigureRegistrationTxHook(hook RegistrationTxHook) error {
	if hook == nil {
		return ErrInvalid
	}
	registrationTxHookState.Lock()
	defer registrationTxHookState.Unlock()
	if registrationTxHookState.hook != nil {
		return ErrConflict
	}
	registrationTxHookState.hook = hook
	return nil
}

func runRegistrationTxHook(ctx context.Context, tx *sql.Tx, user User, correlationID string) error {
	registrationTxHookState.RLock()
	hook := registrationTxHookState.hook
	registrationTxHookState.RUnlock()
	if hook == nil {
		return nil
	}
	return hook(ctx, tx, user, correlationID)
}
