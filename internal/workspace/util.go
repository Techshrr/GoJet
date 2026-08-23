package workspace

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"unicode"
)

func newOpaqueID(prefix string, bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func newInvitationToken() (string, string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func hashInvitationToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func normalizeEmail(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeName(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	space := false
	for _, r := range value {
		if unicode.IsSpace(r) {
			if out.Len() > 0 {
				space = true
			}
			continue
		}
		if space {
			out.WriteByte(' ')
			space = false
		}
		out.WriteRune(unicode.ToLower(r))
	}
	return out.String()
}

func validRole(role string) bool {
	switch role {
	case RoleOwner, RoleAdmin, RoleMember, RoleViewer:
		return true
	default:
		return false
	}
}

func validInvitationRole(role string) bool {
	return role == RoleAdmin || role == RoleMember || role == RoleViewer
}

func canManageWorkspace(role string) bool {
	return role == RoleOwner || role == RoleAdmin
}

func canManageOrganization(role string) bool {
	return role == RoleOwner || role == RoleAdmin
}

func canManageResources(role string) bool {
	return role == RoleOwner || role == RoleAdmin || role == RoleMember
}

func canManageMembers(role string) bool {
	return role == RoleOwner || role == RoleAdmin
}
