package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type SourceCredentialHandler struct {
	credentials *service.SourceCredentialService
}

func NewSourceCredentialHandler(credentials *service.SourceCredentialService) *SourceCredentialHandler {
	return &SourceCredentialHandler{credentials: credentials}
}

func (h *SourceCredentialHandler) CreateSourceCredential(w http.ResponseWriter, r *http.Request) {
	var req api.CreateSourceCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	credential, err := h.credentials.CreateSourceCredential(r.Context(), service.CreateSourceCredentialInput{
		Name:      req.Name,
		Kind:      req.Kind,
		Username:  req.Username,
		SecretRef: req.SecretRef,
	})
	if err != nil {
		h.writeSourceCredentialError(w, err)
		return
	}

	writeDataJSON(w, http.StatusCreated, toSourceCredentialResponse(credential))
}

func (h *SourceCredentialHandler) ListSourceCredentials(w http.ResponseWriter, r *http.Request) {
	credentials, err := h.credentials.ListSourceCredentials(r.Context())
	if err != nil {
		h.writeSourceCredentialError(w, err)
		return
	}

	responses := make([]api.SourceCredentialResponse, 0, len(credentials))
	for _, credential := range credentials {
		responses = append(responses, toSourceCredentialResponse(credential))
	}
	writeDataJSON(w, http.StatusOK, api.SourceCredentialListResponse{Credentials: responses})
}

func (h *SourceCredentialHandler) GetSourceCredential(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "credentialID"))
	credential, err := h.credentials.GetSourceCredential(r.Context(), id)
	if err != nil {
		h.writeSourceCredentialError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toSourceCredentialResponse(credential))
}

func (h *SourceCredentialHandler) UpdateSourceCredential(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "credentialID"))
	var req api.UpdateSourceCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	updated, err := h.credentials.UpdateSourceCredential(r.Context(), id, service.UpdateSourceCredentialInput{
		Name:      req.Name,
		Kind:      req.Kind,
		Username:  service.OptionalStringPatch{Set: req.Username.Set, Value: req.Username.Value},
		SecretRef: req.SecretRef,
	})
	if err != nil {
		h.writeSourceCredentialError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toSourceCredentialResponse(updated))
}

func (h *SourceCredentialHandler) DeleteSourceCredential(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "credentialID"))
	if err := h.credentials.DeleteSourceCredential(r.Context(), id); err != nil {
		h.writeSourceCredentialError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SourceCredentialHandler) writeSourceCredentialError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrSourceCredentialNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, service.ErrSourceCredentialNameRequired) ||
		errors.Is(err, service.ErrSourceCredentialSecretRefRequired) ||
		errors.Is(err, service.ErrSourceCredentialKindInvalid) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func toSourceCredentialResponse(credential domain.SourceCredential) api.SourceCredentialResponse {
	return api.SourceCredentialResponse{
		ID:        credential.ID,
		Name:      credential.Name,
		Kind:      string(credential.Kind),
		Username:  credential.Username,
		SecretRef: credential.SecretRef,
		CreatedAt: credential.CreatedAt.Format(time.RFC3339),
		UpdatedAt: credential.UpdatedAt.Format(time.RFC3339),
	}
}
