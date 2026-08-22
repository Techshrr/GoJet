package files

import (
	"fmt"
	"net/http"
	"strconv"
)

func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	fileID, ok := parseFileID(w, r.PathValue("fileId"))
	if !ok {
		return
	}
	request, ok := decodeChangeRequest(w, r)
	if !ok {
		return
	}
	resource, err := a.store.Delete(r.Context(), workspaceID, fileID, actor.ActorID, fileCorrelationID(r), request.ChangeReason)
	if err != nil {
		writeFileStoreError(w, err)
		return
	}
	if err := a.storage.Remove(resource.StorageKey); err != nil {
		writeFileAPIError(w, http.StatusServiceUnavailable, "storage_cleanup_failed", "The file record was deleted but storage cleanup requires operator attention.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) publish(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	fileID, ok := parseFileID(w, r.PathValue("fileId"))
	if !ok {
		return
	}
	request, ok := decodeChangeRequest(w, r)
	if !ok {
		return
	}
	current, err := a.store.Get(r.Context(), workspaceID, fileID)
	if err != nil {
		writeFileStoreError(w, err)
		return
	}
	if current.ScanState != ScanSafe {
		writeFileStoreError(w, ErrNotSafe)
		return
	}
	moved := false
	if !current.Published {
		if err := a.storage.Publish(current.StorageKey); err != nil {
			writeFileAPIError(w, http.StatusServiceUnavailable, "storage_unavailable", "The file could not be published.")
			return
		}
		moved = true
	}
	resource, err := a.store.MarkPublished(r.Context(), workspaceID, fileID, actor.ActorID, fileCorrelationID(r), request.ChangeReason)
	if err != nil {
		if moved {
			_ = a.storage.ReturnToQuarantine(current.StorageKey)
		}
		writeFileStoreError(w, err)
		return
	}
	writeFileJSON(w, http.StatusOK, resource)
}

func (a *API) rescan(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	fileID, ok := parseFileID(w, r.PathValue("fileId"))
	if !ok {
		return
	}
	request, ok := decodeChangeRequest(w, r)
	if !ok {
		return
	}
	current, err := a.store.Get(r.Context(), workspaceID, fileID)
	if err != nil {
		writeFileStoreError(w, err)
		return
	}
	returned := false
	if current.Published {
		if err := a.storage.ReturnToQuarantine(current.StorageKey); err != nil {
			writeFileAPIError(w, http.StatusServiceUnavailable, "storage_unavailable", "The file could not be returned to quarantine.")
			return
		}
		returned = true
	}
	resource, err := a.store.BeginRescan(r.Context(), workspaceID, fileID, actor.ActorID, fileCorrelationID(r), request.ChangeReason)
	if err != nil {
		if returned {
			_ = a.storage.Publish(current.StorageKey)
		}
		writeFileStoreError(w, err)
		return
	}
	writeFileJSON(w, http.StatusAccepted, resource)
}

func (a *API) download(w http.ResponseWriter, r *http.Request) {
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
	if resource.ScanState != ScanSafe {
		writeFileStoreError(w, ErrNotSafe)
		return
	}
	var file readSeekCloser
	if resource.Published {
		file, err = a.storage.OpenPublished(resource.StorageKey)
	} else {
		file, err = a.storage.OpenQuarantine(resource.StorageKey)
	}
	if err != nil {
		writeFileAPIError(w, http.StatusServiceUnavailable, "storage_unavailable", "The file bytes are unavailable.")
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", resource.DetectedMIME)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="file-%d"`, resource.ID))
	w.Header().Set("Content-Length", strconv.FormatUint(resource.SizeBytes, 10))
	http.ServeContent(w, r, "file", resource.UpdatedAt, file)
}
