package files

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

type readSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}

func decodeChangeRequest(w http.ResponseWriter, r *http.Request) (changeRequest, bool) {
	var request changeRequest
	if !decodeFileJSON(w, r, &request) {
		return changeRequest{}, false
	}
	if strings.TrimSpace(request.ChangeReason) == "" {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_input", "change_reason is required.")
		return changeRequest{}, false
	}
	return request, true
}

func parseFileID(w http.ResponseWriter, value string) (uint64, bool) {
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_file_id", "File ID is invalid.")
		return 0, false
	}
	return id, true
}

func parseOptionalNonNegativeInt(value string, fallback int) (int, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, ErrInvalidInput
	}
	return parsed, nil
}

func decodeFileJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_json", "Request body is invalid.")
		return false
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		writeFileAPIError(w, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON object.")
		return false
	}
	return true
}

func writeFileStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidInput):
		writeFileAPIError(w, http.StatusBadRequest, "invalid_input", "Request validation failed.")
	case errors.Is(err, ErrNotFound):
		writeFileAPIError(w, http.StatusNotFound, "not_found", "File resource not found.")
	case errors.Is(err, ErrDeleted):
		writeFileAPIError(w, http.StatusGone, "deleted", "File resource has been deleted.")
	case errors.Is(err, ErrQuota):
		writeFileAPIError(w, http.StatusTooManyRequests, "quota_reached", "Workspace file quota reached.")
	case errors.Is(err, ErrNotSafe):
		writeFileAPIError(w, http.StatusConflict, "file_not_safe", "Only a current safe scan verdict can authorize file bytes or publication.")
	case errors.Is(err, ErrConflict), errors.Is(err, ErrScanClaimConflict):
		writeFileAPIError(w, http.StatusConflict, "state_conflict", "The file state changed; retry against current state.")
	case errors.Is(err, ErrExpired):
		writeFileAPIError(w, http.StatusGone, "expired", "File resource has expired.")
	case errors.Is(err, ErrDownloadLimit):
		writeFileAPIError(w, http.StatusGone, "download_limit", "File download limit has been reached.")
	default:
		writeFileAPIError(w, http.StatusInternalServerError, "server_error", "The request could not be completed.")
	}
}

func writeFileAPIError(w http.ResponseWriter, status int, code, message string) {
	writeFileJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func writeFileJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
