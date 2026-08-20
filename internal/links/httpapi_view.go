package links

import (
	"encoding/json"
	"errors"
)

type accessRequest struct {
	Password      string `json:"password,omitempty"`
	ClearPassword bool   `json:"clear_password,omitempty"`
}

func createAccessConfig(request accessRequest) (AccessConfig, error) {
	if request.ClearPassword {
		return AccessConfig{}, ErrInvalidInput
	}
	if request.Password == "" {
		return AccessConfig{}, nil
	}
	hash, err := HashLinkPassword(request.Password)
	if err != nil {
		return AccessConfig{}, err
	}
	return AccessConfig{PasswordHash: hash}, nil
}

func updateAccessConfig(current AccessConfig, request accessRequest) (AccessConfig, error) {
	if request.ClearPassword && request.Password != "" {
		return AccessConfig{}, ErrInvalidInput
	}
	if request.ClearPassword {
		return AccessConfig{}, nil
	}
	if request.Password == "" {
		return current, nil
	}
	hash, err := HashLinkPassword(request.Password)
	if err != nil {
		return AccessConfig{}, err
	}
	return AccessConfig{PasswordHash: hash}, nil
}

func publicLink(link Link) map[string]any {
	return map[string]any{
		"id":                  link.ID,
		"workspace_id":        link.WorkspaceID,
		"hostname":            link.Hostname,
		"domain_kind":         link.DomainKind,
		"code":                link.Code,
		"title":               link.Title,
		"primary_destination": link.PrimaryDestination,
		"redirect_status":     link.RedirectStatus,
		"status":              link.Status,
		"version":             link.Version,
		"risk_fingerprint":    link.RiskFingerprint,
		"routing":             link.Routing,
		"ab":                  link.AB,
		"utm":                 link.UTM,
		"access": map[string]any{
			"password_protected": link.Access.PasswordHash != "",
		},
		"expires_at":  link.ExpiresAt,
		"click_limit": link.ClickLimit,
		"click_count": link.ClickCount,
		"one_time":    link.OneTime,
		"created_at":  link.CreatedAt,
		"updated_at":  link.UpdatedAt,
		"deleted_at":  link.DeletedAt,
	}
}

func publicLinks(links []Link) []map[string]any {
	result := make([]map[string]any, 0, len(links))
	for _, link := range links {
		result = append(result, publicLink(link))
	}
	return result
}

func publicHistory(versions []LinkVersion) ([]map[string]any, error) {
	result := make([]map[string]any, 0, len(versions))
	for _, version := range versions {
		var snapshot Link
		if err := json.Unmarshal(version.Snapshot, &snapshot); err != nil {
			return nil, errors.New("decode link history snapshot")
		}
		result = append(result, map[string]any{
			"version":          version.Version,
			"actor_id":         version.ActorID,
			"change_reason":    version.ChangeReason,
			"snapshot":         publicLink(snapshot),
			"risk_fingerprint": version.RiskFingerprint,
			"created_at":       version.CreatedAt,
		})
	}
	return result, nil
}
