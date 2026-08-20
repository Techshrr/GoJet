package links

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	linkPasswordAlgorithm  = "pbkdf2-sha256"
	linkPasswordVersion    = 1
	linkPasswordIterations = 600000
	linkPasswordSaltBytes  = 16
	linkPasswordKeyBytes   = 32
	linkPasswordMinBytes   = 8
	linkPasswordMaxBytes   = 256
)

var ErrInvalidPassword = errors.New("invalid link password")

func validateLinkPassword(password string) error {
	if !utf8.ValidString(password) || len(password) < linkPasswordMinBytes || len(password) > linkPasswordMaxBytes {
		return ErrInvalidPassword
	}
	return nil
}

// HashLinkPassword derives a versioned PBKDF2-HMAC-SHA256 verifier using a
// cryptographically random salt. The caller must persist only the returned
// encoded verifier; plaintext passwords are never stored.
func HashLinkPassword(password string) (string, error) {
	if err := validateLinkPassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, linkPasswordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	derived, err := pbkdf2.Key(sha256.New, password, salt, linkPasswordIterations, linkPasswordKeyBytes)
	if err != nil {
		return "", fmt.Errorf("derive password verifier: %w", err)
	}
	encoding := base64.RawURLEncoding
	return strings.Join([]string{
		linkPasswordAlgorithm,
		strconv.Itoa(linkPasswordVersion),
		strconv.Itoa(linkPasswordIterations),
		encoding.EncodeToString(salt),
		encoding.EncodeToString(derived),
	}, "$"), nil
}

// VerifyLinkPassword validates the encoded verifier strictly and compares the
// derived key in constant time. Malformed or unsupported encodings fail closed.
func VerifyLinkPassword(encoded, password string) bool {
	if err := validateLinkPassword(password); err != nil {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != linkPasswordAlgorithm {
		return false
	}
	version, err := strconv.Atoi(parts[1])
	if err != nil || version != linkPasswordVersion {
		return false
	}
	iterations, err := strconv.Atoi(parts[2])
	if err != nil || iterations != linkPasswordIterations {
		return false
	}
	encoding := base64.RawURLEncoding
	salt, err := encoding.DecodeString(parts[3])
	if err != nil || len(salt) != linkPasswordSaltBytes {
		return false
	}
	expected, err := encoding.DecodeString(parts[4])
	if err != nil || len(expected) != linkPasswordKeyBytes {
		return false
	}
	actual, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(expected))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
