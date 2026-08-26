package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"strings"
	"unicode"
)

type OpaqueSecret struct {
	Value string
	Hash  [32]byte
}

func NewOpaqueSecret(prefix string, entropyBytes int) (OpaqueSecret, error) {
	if entropyBytes < 16 || entropyBytes > 64 || len(prefix) > 24 {
		return OpaqueSecret{}, ErrInvalid
	}
	for _, r := range prefix {
		if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			return OpaqueSecret{}, ErrInvalid
		}
	}
	buf := make([]byte, entropyBytes)
	if _, err := rand.Read(buf); err != nil {
		return OpaqueSecret{}, err
	}
	value := prefix + base64.RawURLEncoding.EncodeToString(buf)
	return OpaqueSecret{Value: value, Hash: HashOpaque(value)}, nil
}

func HashOpaque(value string) [32]byte {
	return sha256.Sum256([]byte(value))
}

func EqualOpaqueHash(left, right [32]byte) bool {
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}

func newOpaqueID(prefix string, entropyBytes int) (string, error) {
	secret, err := NewOpaqueSecret(prefix, entropyBytes)
	if err != nil {
		return "", err
	}
	return secret.Value, nil
}

func NormalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(raw)
	if email == "" || len(email) > 320 || strings.IndexFunc(email, unicode.IsSpace) >= 0 || strings.Count(email, "@") != 1 {
		return "", ErrInvalid
	}
	parts := strings.SplitN(email, "@", 2)
	local := strings.TrimSpace(parts[0])
	domain := strings.TrimSpace(parts[1])
	if local == "" || domain == "" || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") || strings.Contains(domain, "..") {
		return "", ErrInvalid
	}
	return strings.ToLower(local) + "@" + strings.ToLower(domain), nil
}

func ValidProvider(provider string) bool {
	for _, candidate := range Providers {
		if provider == candidate {
			return true
		}
	}
	return false
}
