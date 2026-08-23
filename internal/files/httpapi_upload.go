package files

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
)

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("workspaceId")
	actor, ok := a.authenticate(w, r, workspaceID, true)
	if !ok {
		return
	}
	reader, err := r.MultipartReader()
	if err != nil {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_multipart", "A multipart upload is required.")
		return
	}

	var (
		storageKey string
		size       uint64
		sha256sum  string
		original   string
		declared   string
		detected   string
		reason     string
		gotFile    bool
		gotReason  bool
	)
	cleanup := false
	defer func() {
		if cleanup && storageKey != "" {
			_ = a.storage.Remove(storageKey)
		}
	}()

	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			writeFileAPIError(w, http.StatusBadRequest, "invalid_multipart", "The multipart upload could not be read.")
			return
		}
		switch part.FormName() {
		case "change_reason":
			if gotReason {
				_ = part.Close()
				writeFileAPIError(w, http.StatusBadRequest, "duplicate_field", "change_reason must appear exactly once.")
				return
			}
			gotReason = true
			raw, readErr := io.ReadAll(io.LimitReader(part, 4097))
			_ = part.Close()
			if readErr != nil || len(raw) > 4096 {
				writeFileAPIError(w, http.StatusBadRequest, "invalid_change_reason", "change_reason is invalid.")
				return
			}
			reason = strings.TrimSpace(string(raw))
		case "file":
			if gotFile || part.FileName() == "" {
				_ = part.Close()
				writeFileAPIError(w, http.StatusBadRequest, "invalid_file", "Exactly one file part is required.")
				return
			}
			gotFile = true
			prefix := make([]byte, 512)
			n, readErr := io.ReadFull(part, prefix)
			if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
				_ = part.Close()
				writeFileAPIError(w, http.StatusBadRequest, "invalid_file", "The uploaded file could not be read.")
				return
			}
			prefix = prefix[:n]
			if len(prefix) == 0 {
				_ = part.Close()
				writeFileAPIError(w, http.StatusBadRequest, "invalid_file", "Empty files are not accepted.")
				return
			}
			original, declared, detected, err = a.policy.Validate(part.FileName(), part.Header.Get("Content-Type"), prefix)
			if err != nil {
				_ = part.Close()
				writeFileAPIError(w, http.StatusBadRequest, "file_type_denied", "The uploaded file type is not permitted.")
				return
			}
			storageKey, err = NewStorageKey()
			if err != nil {
				_ = part.Close()
				writeFileAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
				return
			}
			size, sha256sum, err = a.storage.WriteQuarantine(storageKey, io.MultiReader(bytes.NewReader(prefix), part), a.maxUploadBytes)
			_ = part.Close()
			if err != nil {
				writeFileStoreError(w, err)
				return
			}
			cleanup = true
		default:
			_ = part.Close()
			writeFileAPIError(w, http.StatusBadRequest, "unknown_field", "The multipart upload contains an unsupported field.")
			return
		}
	}
	if !gotFile || !gotReason || reason == "" {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_input", "file and change_reason are required.")
		return
	}
	slug, err := NewPublicSlug()
	if err != nil {
		writeFileAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
		return
	}
	resource, err := a.store.CreateQuarantined(r.Context(), CreateInput{
		WorkspaceID: workspaceID, PublicSlug: slug, OriginalName: original, StorageKey: storageKey,
		SizeBytes: size, ContentSHA256: sha256sum, DeclaredMIME: declared, DetectedMIME: detected,
		CreatedBy: actor.ActorID, CorrelationID: fileCorrelationID(r), Reason: reason,
	})
	if err != nil {
		writeFileStoreError(w, err)
		return
	}
	cleanup = false
	writeFileJSON(w, http.StatusCreated, resource)
}
