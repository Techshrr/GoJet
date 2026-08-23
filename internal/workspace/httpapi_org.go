package workspace

import (
	"net/http"
	"strings"
)

func (a *API) getOrganization(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	item, err := a.store.GetOrganization(r.Context(), actor.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) patchOrganization(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin) {
		a.audit(r, actor.WorkspaceID, p.UserID, "organization.update", "organization", actor.WorkspaceID, "", ErrForbidden)
		return
	}
	var input struct {
		Name            string `json:"name"`
		Description     string `json:"description"`
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.store.UpdateOrganization(r.Context(), actor.WorkspaceID, input.Name, input.Description, input.ExpectedVersion)
	a.audit(r, actor.WorkspaceID, p.UserID, "organization.update", "organization", actor.WorkspaceID, "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) listCampaigns(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	items, err := a.store.ListCampaigns(r.Context(), actor.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createCampaign(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "campaign.create", "campaign", "", "", ErrForbidden)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.store.CreateCampaign(r.Context(), actor.WorkspaceID, p.UserID, input.Name)
	a.audit(r, actor.WorkspaceID, p.UserID, "campaign.create", "campaign", item.ID, "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) patchCampaign(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "campaign.update", "campaign", r.PathValue("campaignId"), "", ErrForbidden)
		return
	}
	var input struct {
		Name            string `json:"name"`
		Status          string `json:"status"`
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	id := strings.TrimSpace(r.PathValue("campaignId"))
	item, err := a.store.UpdateCampaign(r.Context(), actor.WorkspaceID, id, input.Name, input.Status, input.ExpectedVersion)
	a.audit(r, actor.WorkspaceID, p.UserID, "campaign.update", "campaign", id, "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteCampaign(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "campaign.delete", "campaign", r.PathValue("campaignId"), "", ErrForbidden)
		return
	}
	id := strings.TrimSpace(r.PathValue("campaignId"))
	err := a.store.DeleteCampaign(r.Context(), actor.WorkspaceID, id)
	a.audit(r, actor.WorkspaceID, p.UserID, "campaign.delete", "campaign", id, "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listTags(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	items, err := a.store.ListTags(r.Context(), actor.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createTag(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "tag.create", "tag", "", "", ErrForbidden)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.store.CreateTag(r.Context(), actor.WorkspaceID, p.UserID, input.Name)
	a.audit(r, actor.WorkspaceID, p.UserID, "tag.create", "tag", "", "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) patchTag(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "tag.update", "tag", r.PathValue("tagId"), "", ErrForbidden)
		return
	}
	id, ok := pathUint64(w, r, "tagId")
	if !ok {
		return
	}
	var input struct {
		Name            string `json:"name"`
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.store.UpdateTag(r.Context(), actor.WorkspaceID, id, input.Name, input.ExpectedVersion)
	a.audit(r, actor.WorkspaceID, p.UserID, "tag.update", "tag", r.PathValue("tagId"), "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteTag(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "tag.delete", "tag", r.PathValue("tagId"), "", ErrForbidden)
		return
	}
	id, ok := pathUint64(w, r, "tagId")
	if !ok {
		return
	}
	err := a.store.DeleteTag(r.Context(), actor.WorkspaceID, id)
	a.audit(r, actor.WorkspaceID, p.UserID, "tag.delete", "tag", r.PathValue("tagId"), "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listFolders(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	items, err := a.store.ListFolders(r.Context(), actor.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createFolder(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "folder.create", "folder", "", "", ErrForbidden)
		return
	}
	var input struct {
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.store.CreateFolder(r.Context(), actor.WorkspaceID, p.UserID, input.Name)
	a.audit(r, actor.WorkspaceID, p.UserID, "folder.create", "folder", "", "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (a *API) patchFolder(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "folder.update", "folder", r.PathValue("folderId"), "", ErrForbidden)
		return
	}
	id, ok := pathUint64(w, r, "folderId")
	if !ok {
		return
	}
	var input struct {
		Name            string `json:"name"`
		ExpectedVersion uint64 `json:"expected_version"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.store.UpdateFolder(r.Context(), actor.WorkspaceID, id, input.Name, input.ExpectedVersion)
	a.audit(r, actor.WorkspaceID, p.UserID, "folder.update", "folder", r.PathValue("folderId"), "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteFolder(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "folder.delete", "folder", r.PathValue("folderId"), "", ErrForbidden)
		return
	}
	id, ok := pathUint64(w, r, "folderId")
	if !ok {
		return
	}
	err := a.store.DeleteFolder(r.Context(), actor.WorkspaceID, id)
	a.audit(r, actor.WorkspaceID, p.UserID, "folder.delete", "folder", r.PathValue("folderId"), "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) patchLinkOrganization(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin, RoleMember) {
		a.audit(r, actor.WorkspaceID, p.UserID, "links.organization.update", "links", "", "", ErrForbidden)
		return
	}
	var input struct {
		LinkIDs    []uint64 `json:"link_ids"`
		CampaignID *string  `json:"campaign_id"`
		FolderID   *uint64  `json:"folder_id"`
		TagIDs     []uint64 `json:"tag_ids"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	err := a.store.UpdateLinkOrganization(r.Context(), actor.WorkspaceID, LinkOrganizationUpdate{
		LinkIDs: input.LinkIDs, CampaignID: input.CampaignID, FolderID: input.FolderID, TagIDs: input.TagIDs,
	})
	a.audit(r, actor.WorkspaceID, p.UserID, "links.organization.update", "links", "", "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	items := make([]LinkOrganization, 0, len(input.LinkIDs))
	for _, linkID := range input.LinkIDs {
		item, err := a.store.LinkOrganization(r.Context(), actor.WorkspaceID, linkID)
		if err != nil {
			writeWorkspaceStoreError(w, r, err)
			return
		}
		items = append(items, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
