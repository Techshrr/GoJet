package support

import (
	"crypto/sha256"
	"strings"
)

const submissionIdempotencyDomain = "gojet:p14:submission-idempotency:v1"

// SubmissionIdempotencyHash scopes a client idempotency key to one protected
// operation/authority boundary. Only the digest is suitable for durable storage;
// callers must not persist or log rawKey.
func SubmissionIdempotencyHash(surface SubmissionSurface, authorityScope, rawKey string) ([32]byte, error) {
	if !validSubmissionSurface(surface) {
		return [32]byte{}, ErrInvalidInput
	}
	authorityScope = strings.TrimSpace(authorityScope)
	rawKey = strings.TrimSpace(rawKey)
	if authorityScope == "" || rawKey == "" || len(authorityScope) > 512 || len(rawKey) > 512 {
		return [32]byte{}, ErrInvalidInput
	}

	h := sha256.New()
	writeHashPart(h, submissionIdempotencyDomain)
	writeHashPart(h, string(surface))
	writeHashPart(h, authorityScope)
	writeHashPart(h, rawKey)
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, nil
}
