package securetoken

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

var ErrInvalidKey = errors.New("invalid secure token key")

// Key derives opaque one-time material from a non-secret identifier without
// persisting the raw token. The secret bytes are runtime configuration only.
type Key struct {
	id     string
	secret [32]byte
}

func NewKey(id string, secret []byte) (Key, error) {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 64 || len(secret) != 32 {
		return Key{}, ErrInvalidKey
	}
	for _, r := range id {
		if !(r == '-' || r == '_' || r == '.' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return Key{}, ErrInvalidKey
		}
	}
	var out Key
	out.id = id
	copy(out.secret[:], secret)
	return out, nil
}

func (k Key) ID() string {
	return k.id
}

func (k Key) Derive(prefix, purpose, identifier string) (string, error) {
	prefix = strings.TrimSpace(prefix)
	purpose = strings.TrimSpace(purpose)
	identifier = strings.TrimSpace(identifier)
	if k.id == "" || prefix == "" || purpose == "" || identifier == "" || len(prefix) > 16 || len(purpose) > 64 || len(identifier) > 128 {
		return "", ErrInvalidKey
	}
	mac := hmac.New(sha256.New, k.secret[:])
	writePart(mac, purpose)
	writePart(mac, identifier)
	return prefix + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func Hash(raw string) [32]byte {
	return sha256.Sum256([]byte(raw))
}

func writePart(dst interface{ Write([]byte) (int, error) }, value string) {
	_, _ = dst.Write([]byte{byte(len(value) >> 24), byte(len(value) >> 16), byte(len(value) >> 8), byte(len(value))})
	_, _ = dst.Write([]byte(value))
}
