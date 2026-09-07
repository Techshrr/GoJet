package main

import (
	"database/sql"
	"net/http"
	"os"

	"github.com/Techshrr/GoJet/internal/workspace"
	"github.com/redis/go-redis/v9"
)

func buildWorkspaceHandler(db *sql.DB, redisClient *redis.Client, testAuth bool) (http.Handler, bool, error) {
	if os.Getenv("GOJET_WORKSPACE_ENABLED") != "1" {
		return nil, false, nil
	}
	store := workspace.NewStore(db)
	if testAuth {
		return workspace.NewAPI(store, true).Handler(), true, nil
	}
	authority, err := buildWorkspaceSessionAuthority(db, redisClient)
	if err != nil {
		return nil, false, err
	}
	return workspace.NewAPIWithPrincipalResolver(store, authority.resolve).Handler(), true, nil
}

func mountWorkspaceRoutes(root *http.ServeMux, handler http.Handler) {
	patterns := []string{
		"GET /api/workspaces",
		"POST /api/workspaces",
		"GET /api/workspaces/{workspaceId}",
		"PATCH /api/workspaces/{workspaceId}",
		"GET /api/workspaces/{workspaceId}/overview",
		"GET /api/workspaces/{workspaceId}/members",
		"PATCH /api/workspaces/{workspaceId}/members/{memberId}",
		"DELETE /api/workspaces/{workspaceId}/members/{memberId}",
		"GET /api/workspaces/{workspaceId}/invitations",
		"POST /api/workspaces/{workspaceId}/invitations",
		"DELETE /api/workspaces/{workspaceId}/invitations/{invitationId}",
		"GET /api/invitations/{token}",
		"POST /api/invitations/accept",
		"POST /api/invitations/reject",
		"GET /api/workspaces/{workspaceId}/organization",
		"PATCH /api/workspaces/{workspaceId}/organization",
		"GET /api/workspaces/{workspaceId}/campaigns",
		"POST /api/workspaces/{workspaceId}/campaigns",
		"PATCH /api/workspaces/{workspaceId}/campaigns/{campaignId}",
		"DELETE /api/workspaces/{workspaceId}/campaigns/{campaignId}",
		"GET /api/workspaces/{workspaceId}/tags",
		"POST /api/workspaces/{workspaceId}/tags",
		"PATCH /api/workspaces/{workspaceId}/tags/{tagId}",
		"DELETE /api/workspaces/{workspaceId}/tags/{tagId}",
		"GET /api/workspaces/{workspaceId}/folders",
		"POST /api/workspaces/{workspaceId}/folders",
		"PATCH /api/workspaces/{workspaceId}/folders/{folderId}",
		"DELETE /api/workspaces/{workspaceId}/folders/{folderId}",
		"PATCH /api/workspaces/{workspaceId}/links/organization",
		"GET /api/workspaces/{workspaceId}/notifications",
		"POST /api/workspaces/{workspaceId}/notifications/{notificationId}/read",
		"POST /api/workspaces/{workspaceId}/notifications/{notificationId}/unread",
		"POST /api/workspaces/{workspaceId}/notifications/read-all",
	}
	for _, pattern := range patterns {
		root.Handle(pattern, handler)
	}
}
