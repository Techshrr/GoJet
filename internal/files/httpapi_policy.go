package files

import (
	"net/http"
	"strings"

	"github.com/Techshrr/GoJet/internal/links"
)

func (a *API) get(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	if _, ok := a.authenticate(w, r, workspaceID, false); !ok {
		return
	}
	fileID, ok := parseFileID(w, r.PathValue("fileId"))
	if !ok {
		return
	}
	resource, err := a.store.Get(r.Context(), workspaceID, fileID)
	if err != nil {
		writeFileStoreError(w, err)
		return
	}
	writeFileJSON(w, http.StatusOK, resource)
}

func (a *API) patch(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	fileID, ok := parseFileID(w, r.PathValue("fileId"))
	if !ok {
		return
	}
	var request policyPatchRequest
	if !decodeFileJSON(w, r, &request) {
		return
	}
	hasChange := request.Password != nil || request.ClearPassword ||
		request.ExpiresAt != nil || request.ClearExpiresAt ||
		request.RetentionUntil != nil || request.ClearRetentionUntil ||
		request.DownloadLimit != nil || request.ClearDownloadLimit
	if strings.TrimSpace(request.ChangeReason) == "" || !hasChange ||
		(request.Password != nil && request.ClearPassword) ||
		(request.ExpiresAt != nil && request.ClearExpiresAt) ||
		(request.RetentionUntil != nil && request.ClearRetentionUntil) ||
		(request.DownloadLimit != nil && request.ClearDownloadLimit) {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_input", "The access policy update is invalid.")
		return
	}
	var passwordHash *string
	if request.Password != nil {
		hash, err := links.HashLinkPassword(*request.Password)
		if err != nil {
			writeFileAPIError(w, http.StatusBadRequest, "invalid_password", "Password does not meet the password policy.")
			return
		}
		passwordHash = &hash
	}
	if request.DownloadLimit != nil && *request.DownloadLimit == 0 {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_download_limit", "download_limit must be greater than zero.")
		return
	}
	resource, err := a.store.UpdateAccessPolicy(r.Context(), AccessPolicyInput{
		WorkspaceID: workspaceID, FileID: fileID, ActorID: actor.ActorID, CorrelationID: fileCorrelationID(r),
		Reason: request.ChangeReason, PasswordHash: passwordHash, ClearPassword: request.ClearPassword,
		ExpiresAt: request.ExpiresAt, ClearExpiresAt: request.ClearExpiresAt,
		RetentionUntil: request.RetentionUntil, ClearRetentionUntil: request.ClearRetentionUntil,
		DownloadLimit: request.DownloadLimit, ClearDownloadLimit: request.ClearDownloadLimit,
	})
	if err != nil {
		writeFileStoreError(w, err)
		return
	}
	writeFileJSON(w, http.StatusOK, resource)
}
