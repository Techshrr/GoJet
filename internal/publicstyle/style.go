// Package publicstyle shares canonical Design System tokens with native public pages.
package publicstyle

import (
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
)

// tokens.css is an exact copy of frontend/packages/tokens/generated/tokens.css.
// The parity test prevents the embedded runtime copy from drifting.
//go:embed tokens.css
var tokens string

//go:embed public.css
var layout string

// CSS returns trusted application CSS, never user-provided content.
func CSS() string { return tokens + "\n" + layout }

// CSP permits only the exact embedded stylesheet and same-origin form actions.
func CSP() string {
	digest := sha256.Sum256([]byte(CSS()))
	return "default-src 'none'; style-src 'sha256-" + base64.StdEncoding.EncodeToString(digest[:]) + "'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"
}
