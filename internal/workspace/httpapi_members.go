package workspace

import (
	"net/http"
	"strings"
	"time"
)

func (a *API) listMembers(w http.ResponseWriter, r *http.Request) {
	_, m, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	items, err := a.store.ListMembers(r.Context(), m.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	invitations := []Invitation{}
	if m.Role == RoleOwner || m.Role == RoleAdmin {
		invitations, err = a.store.ListInvitations(r.Context(), m.WorkspaceID)
		if err != nil {
			writeWorkspaceStoreError(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"members": items, "invitations": invitations})
}

func (a *API) patchMember(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin) {
		a.audit(r, actor.WorkspaceID, p.UserID, "membership.role.update", "membership", r.PathValue("memberId"), "", ErrForbidden)
		return
	}
	memberID, ok := pathUint64(w, r, "memberId")
	if !ok {
		return
	}
	var input struct {
		Role   string `json:"role"`
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	item, err := a.store.UpdateMemberRole(r.Context(), actor.WorkspaceID, memberID, actor.Role, strings.TrimSpace(input.Role))
	a.audit(r, actor.WorkspaceID, p.UserID, "membership.role.update", "membership", r.PathValue("memberId"), input.Reason, err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) deleteMember(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin) {
		a.audit(r, actor.WorkspaceID, p.UserID, "membership.remove", "membership", r.PathValue("memberId"), "", ErrForbidden)
		return
	}
	memberID, ok := pathUint64(w, r, "memberId")
	if !ok {
		return
	}
	var input struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 {
		if !decodeJSON(w, r, &input) {
			return
		}
	}
	err := a.store.RemoveMember(r.Context(), actor.WorkspaceID, memberID, actor.Role)
	a.audit(r, actor.WorkspaceID, p.UserID, "membership.remove", "membership", r.PathValue("memberId"), input.Reason, err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listInvitations(w http.ResponseWriter, r *http.Request) {
	_, actor, ok := a.workspaceActor(w, r, false)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin) {
		return
	}
	items, err := a.store.ListInvitations(r.Context(), actor.WorkspaceID)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (a *API) createInvitation(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin) {
		a.audit(r, actor.WorkspaceID, p.UserID, "invitation.create", "invitation", "", "", ErrForbidden)
		return
	}
	var input struct {
		Email     string    `json:"email"`
		Role      string    `json:"role"`
		ExpiresAt time.Time `json:"expires_at"`
		Reason    string    `json:"reason"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if actor.Role == RoleAdmin && input.Role == RoleOwner {
		a.audit(r, actor.WorkspaceID, p.UserID, "invitation.create", "invitation", "", input.Reason, ErrForbidden)
		writeWorkspaceStoreError(w, r, ErrForbidden)
		return
	}
	created, err := a.store.CreateInvitationForEmail(r.Context(), actor.WorkspaceID, p.UserID, input.Email, input.Role, input.ExpiresAt)
	a.audit(r, actor.WorkspaceID, p.UserID, "invitation.create", "invitation", "", input.Reason, err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

func (a *API) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	p, actor, ok := a.workspaceActor(w, r, true)
	if !ok {
		return
	}
	if !requireRole(w, r, actor, RoleOwner, RoleAdmin) {
		a.audit(r, actor.WorkspaceID, p.UserID, "invitation.revoke", "invitation", r.PathValue("invitationId"), "", ErrForbidden)
		return
	}
	id, ok := pathUint64(w, r, "invitationId")
	if !ok {
		return
	}
	err := a.store.RevokeInvitation(r.Context(), actor.WorkspaceID, id)
	a.audit(r, actor.WorkspaceID, p.UserID, "invitation.revoke", "invitation", r.PathValue("invitationId"), "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) inspectInvitation(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	item, err := a.store.InspectInvitation(r.Context(), r.PathValue("token"), p.Email)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	inspection, inspectErr := a.store.InspectInvitation(r.Context(), input.Token, p.Email)
	if inspectErr != nil {
		writeWorkspaceStoreError(w, r, inspectErr)
		return
	}
	item, err := a.store.AcceptInvitation(r.Context(), input.Token, p)
	a.audit(r, inspection.WorkspaceID, p.UserID, "invitation.accept", "invitation", "", "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (a *API) rejectInvitation(w http.ResponseWriter, r *http.Request) {
	p, ok := a.principal(w, r)
	if !ok {
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	inspection, inspectErr := a.store.InspectInvitation(r.Context(), input.Token, p.Email)
	if inspectErr != nil {
		writeWorkspaceStoreError(w, r, inspectErr)
		return
	}
	err := a.store.RejectInvitation(r.Context(), input.Token, p)
	a.audit(r, inspection.WorkspaceID, p.UserID, "invitation.reject", "invitation", "", "", err)
	if err != nil {
		writeWorkspaceStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
