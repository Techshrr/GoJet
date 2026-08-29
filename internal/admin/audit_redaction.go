package admin

import "strings"

var auditSensitiveKeyFragments = []string{
	"secret", "token", "password", "credential", "authorization", "cookie", "provider_evidence", "evidence_json", "private_key", "dsn",
}

func redactAuditEventsForResponse(items []AuditEvent) []AuditEvent {
	out := make([]AuditEvent, 0, len(items))
	for _, item := range items {
		copyItem := item
		copyItem.Reason = redactAuditReason(item.Reason)
		copyItem.Before = redactAuditMap(item.Before)
		copyItem.After = redactAuditMap(item.After)
		copyItem.Metadata = redactAuditMap(item.Metadata)
		out = append(out, copyItem)
	}
	return out
}

func redactAuditMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		lower := strings.ToLower(key)
		if auditSensitiveKey(lower) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = redactAuditValue(value)
	}
	return out
}

func redactAuditValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return redactAuditMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactAuditValue(item))
		}
		return out
	case string:
		if auditSensitiveString(typed) {
			return "[redacted]"
		}
		return typed
	default:
		return typed
	}
}

func auditSensitiveKey(lower string) bool {
	for _, fragment := range auditSensitiveKeyFragments {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func auditSensitiveString(value string) bool {
	lower := strings.ToLower(value)
	for _, fragment := range []string{"authorization: bearer", "bearer ", "client_secret", "session_token", "private_key", "provider evidence"} {
		if strings.Contains(lower, fragment) {
			return true
		}
	}
	return false
}

func redactAuditReason(value string) string {
	if auditSensitiveString(value) {
		return "[redacted]"
	}
	return value
}
