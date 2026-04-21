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

type ManagedImageSettingsHandler struct {
	settings *service.ManagedImageSettingsService
}

func NewManagedImageSettingsHandler(settings *service.ManagedImageSettingsService) *ManagedImageSettingsHandler {
	return &ManagedImageSettingsHandler{settings: settings}
}

func (h *ManagedImageSettingsHandler) CreateSourceCredential(w http.ResponseWriter, r *http.Request) {
	var req api.CreateSourceCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	credential, err := h.settings.CreateSourceCredential(r.Context(), service.CreateSourceCredentialInput{
		ProjectID: req.ProjectID,
		Name:      req.Name,
		Kind:      req.Kind,
		Username:  req.Username,
		SecretRef: req.SecretRef,
	})
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	writeDataJSON(w, http.StatusCreated, toSourceCredentialResponse(credential))
}

func (h *ManagedImageSettingsHandler) ListSourceCredentials(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	credentials, err := h.settings.ListSourceCredentials(r.Context(), projectID)
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	responses := make([]api.SourceCredentialResponse, 0, len(credentials))
	for _, credential := range credentials {
		responses = append(responses, toSourceCredentialResponse(credential))
	}
	writeDataJSON(w, http.StatusOK, api.SourceCredentialListResponse{Credentials: responses})
}

func (h *ManagedImageSettingsHandler) GetSourceCredential(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "credentialID"))
	credential, err := h.settings.GetSourceCredential(r.Context(), id)
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toSourceCredentialResponse(credential))
}

func (h *ManagedImageSettingsHandler) UpdateSourceCredential(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "credentialID"))
	var req api.UpdateSourceCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	updated, err := h.settings.UpdateSourceCredential(r.Context(), id, service.UpdateSourceCredentialInput{
		Name:      req.Name,
		Kind:      req.Kind,
		Username:  req.Username,
		SecretRef: req.SecretRef,
	})
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toSourceCredentialResponse(updated))
}

func (h *ManagedImageSettingsHandler) DeleteSourceCredential(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "credentialID"))
	if err := h.settings.DeleteSourceCredential(r.Context(), id); err != nil {
		h.writeSettingsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ManagedImageSettingsHandler) CreateRepoWritebackConfig(w http.ResponseWriter, r *http.Request) {
	var req api.CreateRepoWritebackConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	cfg, err := h.settings.CreateRepoWritebackConfig(r.Context(), service.CreateRepoWritebackConfigInput{
		ProjectID:         req.ProjectID,
		RepositoryURL:     req.RepositoryURL,
		PipelinePath:      req.PipelinePath,
		ManagedImageName:  req.ManagedImageName,
		WriteCredentialID: req.WriteCredentialID,
		BotBranchPrefix:   req.BotBranchPrefix,
		CommitAuthorName:  req.CommitAuthorName,
		CommitAuthorEmail: req.CommitAuthorEmail,
		Enabled:           req.Enabled,
	})
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	writeDataJSON(w, http.StatusCreated, toRepoWritebackConfigResponse(cfg))
}

func (h *ManagedImageSettingsHandler) ListRepoWritebackConfigs(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	configs, err := h.settings.ListRepoWritebackConfigs(r.Context(), projectID)
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	responses := make([]api.RepoWritebackConfigResponse, 0, len(configs))
	for _, cfg := range configs {
		responses = append(responses, toRepoWritebackConfigResponse(cfg))
	}
	writeDataJSON(w, http.StatusOK, api.RepoWritebackConfigListResponse{Configs: responses})
}

func (h *ManagedImageSettingsHandler) GetRepoWritebackConfig(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "configID"))
	cfg, err := h.settings.GetRepoWritebackConfig(r.Context(), id)
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toRepoWritebackConfigResponse(cfg))
}

func (h *ManagedImageSettingsHandler) UpdateRepoWritebackConfig(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "configID"))
	var req api.UpdateRepoWritebackConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}

	updated, err := h.settings.UpdateRepoWritebackConfig(r.Context(), id, service.UpdateRepoWritebackConfigInput{
		RepositoryURL:     req.RepositoryURL,
		PipelinePath:      req.PipelinePath,
		ManagedImageName:  req.ManagedImageName,
		WriteCredentialID: req.WriteCredentialID,
		BotBranchPrefix:   req.BotBranchPrefix,
		CommitAuthorName:  req.CommitAuthorName,
		CommitAuthorEmail: req.CommitAuthorEmail,
		Enabled:           req.Enabled,
	})
	if err != nil {
		h.writeSettingsError(w, err)
		return
	}

	writeDataJSON(w, http.StatusOK, toRepoWritebackConfigResponse(updated))
}

func (h *ManagedImageSettingsHandler) DeleteRepoWritebackConfig(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "configID"))
	if err := h.settings.DeleteRepoWritebackConfig(r.Context(), id); err != nil {
		h.writeSettingsError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ManagedImageSettingsHandler) writeSettingsError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrSourceCredentialNotFound) || errors.Is(err, repository.ErrRepoWritebackConfigNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if isManagedSettingsBadRequest(err) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func isManagedSettingsBadRequest(err error) bool {
	return errors.Is(err, service.ErrManagedImageSettingsProjectIDRequired) ||
		errors.Is(err, service.ErrManagedImageSettingsNameRequired) ||
		errors.Is(err, service.ErrManagedImageSettingsRepositoryURLRequired) ||
		errors.Is(err, service.ErrManagedImageSettingsPipelinePathRequired) ||
		errors.Is(err, service.ErrManagedImageSettingsManagedImageNameRequired) ||
		errors.Is(err, service.ErrManagedImageSettingsWriteCredentialIDRequired) ||
		errors.Is(err, service.ErrManagedImageSettingsSecretRefRequired) ||
		errors.Is(err, service.ErrManagedImageSettingsCredentialKindInvalid)
}

func toSourceCredentialResponse(credential domain.SourceCredential) api.SourceCredentialResponse {
	return api.SourceCredentialResponse{
		ID:        credential.ID,
		ProjectID: credential.ProjectID,
		Name:      credential.Name,
		Kind:      string(credential.Kind),
		Username:  credential.Username,
		SecretRef: credential.SecretRef,
		CreatedAt: credential.CreatedAt.Format(time.RFC3339),
		UpdatedAt: credential.UpdatedAt.Format(time.RFC3339),
	}
}

func toRepoWritebackConfigResponse(cfg domain.RepoWritebackConfig) api.RepoWritebackConfigResponse {
	return api.RepoWritebackConfigResponse{
		ID:                cfg.ID,
		ProjectID:         cfg.ProjectID,
		RepositoryURL:     cfg.RepositoryURL,
		PipelinePath:      cfg.PipelinePath,
		ManagedImageName:  cfg.ManagedImageName,
		WriteCredentialID: cfg.WriteCredentialID,
		BotBranchPrefix:   cfg.BotBranchPrefix,
		CommitAuthorName:  cfg.CommitAuthorName,
		CommitAuthorEmail: cfg.CommitAuthorEmail,
		Enabled:           cfg.Enabled,
		CreatedAt:         cfg.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         cfg.UpdatedAt.Format(time.RFC3339),
	}
}
