package files

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

type TypePolicy struct {
	allowed map[string]map[string]struct{}
}

// ParseTypePolicy parses a server-owned allowlist in the form:
//
//	pdf=application/pdf;png=image/png;jpg=image/jpeg|image/pjpeg
//
// Extension names are case-insensitive and may be supplied with or without a dot.
func ParseTypePolicy(raw string) (*TypePolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrInvalidInput
	}
	policy := &TypePolicy{allowed: make(map[string]map[string]struct{})}
	for _, entry := range strings.Split(raw, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			return nil, ErrInvalidInput
		}
		ext := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(parts[0])), ".")
		if ext == "" || strings.ContainsAny(ext, `/\\`) || strings.ContainsAny(ext, " \t\r\n") {
			return nil, ErrInvalidInput
		}
		mimes := make(map[string]struct{})
		for _, rawMIME := range strings.Split(parts[1], "|") {
			normalized, err := normalizeMIME(rawMIME)
			if err != nil {
				return nil, ErrInvalidInput
			}
			mimes[normalized] = struct{}{}
		}
		if len(mimes) == 0 {
			return nil, ErrInvalidInput
		}
		if _, duplicate := policy.allowed[ext]; duplicate {
			return nil, ErrInvalidInput
		}
		policy.allowed[ext] = mimes
	}
	if len(policy.allowed) == 0 {
		return nil, ErrInvalidInput
	}
	return policy, nil
}

func normalizeMIME(raw string) (string, error) {
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(raw))
	if err != nil || mediaType == "" || !strings.Contains(mediaType, "/") {
		return "", ErrInvalidInput
	}
	return strings.ToLower(mediaType), nil
}

func validateOriginalName(raw string) (string, string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || name == "." || name == ".." || !utf8.ValidString(name) || len([]rune(name)) > 255 || strings.ContainsAny(name, `/\\`) {
		return "", "", ErrInvalidInput
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", "", ErrInvalidInput
		}
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(name)), ".")
	if ext == "" {
		return "", "", ErrInvalidInput
	}
	return name, ext, nil
}

func (p *TypePolicy) Validate(originalName, declaredMIME string, prefix []byte) (string, string, string, error) {
	if p == nil || len(p.allowed) == 0 || len(prefix) == 0 {
		return "", "", "", ErrInvalidInput
	}
	name, ext, err := validateOriginalName(originalName)
	if err != nil {
		return "", "", "", err
	}
	allowed, ok := p.allowed[ext]
	if !ok {
		return "", "", "", ErrInvalidInput
	}
	declared, err := normalizeMIME(declaredMIME)
	if err != nil {
		return "", "", "", err
	}
	if _, ok := allowed[declared]; !ok {
		return "", "", "", ErrInvalidInput
	}
	detected, err := normalizeMIME(http.DetectContentType(prefix))
	if err != nil {
		return "", "", "", err
	}
	if _, ok := allowed[detected]; !ok {
		return "", "", "", ErrInvalidInput
	}
	return name, declared, detected, nil
}

func NewPublicSlug() (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	slug := base64.RawURLEncoding.EncodeToString(raw[:])
	if slug == "" || len(slug) > 64 {
		return "", errors.New("public slug generation failed")
	}
	return slug, nil
}
