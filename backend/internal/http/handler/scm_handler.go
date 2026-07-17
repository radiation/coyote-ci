package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/auth"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type scmAdminService interface {
	ListConnections(ctx context.Context) ([]domain.SCMConnectionDetail, error)
	GetConnection(ctx context.Context, id string) (domain.SCMConnectionDetail, error)
	CreateGitHubAppInstallationConnection(ctx context.Context, input service.CreateGitHubAppInstallationConnectionInput) (domain.SCMConnectionDetail, error)
	SetConnectionEnabled(ctx context.Context, id string, enabled *bool) (domain.SCMConnectionDetail, error)
	ListRegisteredRepositories(ctx context.Context) ([]domain.SCMRepositoryRegistration, error)
	GetRegisteredRepository(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error)
	CreateRegisteredRepository(ctx context.Context, input service.CreateSCMRepositoryRegistrationInput) (domain.SCMRepositoryRegistration, error)
}

type SCMHandler struct {
	admin    scmAdminService
	authMode auth.Mode
}

func NewSCMHandler(admin scmAdminService) *SCMHandler {
	return &SCMHandler{admin: admin}
}

func (h *SCMHandler) SetAuthorization(mode auth.Mode) {
	h.authMode = mode
}

func (h *SCMHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	connections, err := h.admin.ListConnections(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	responses := make([]api.SCMConnectionResponse, 0, len(connections))
	for _, item := range connections {
		responses = append(responses, toSCMConnectionResponse(item))
	}
	writeDataJSON(w, http.StatusOK, api.SCMConnectionListResponse{Connections: responses})
}

func (h *SCMHandler) CreateGitHubAppInstallationConnection(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.CreateGitHubAppInstallationConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	connection, err := h.admin.CreateGitHubAppInstallationConnection(r.Context(), service.CreateGitHubAppInstallationConnectionInput{
		DisplayName:         req.DisplayName,
		DeploymentKind:      req.DeploymentKind,
		APIBaseURL:          req.APIBaseURL,
		WebBaseURL:          req.WebBaseURL,
		AppID:               req.AppID,
		AppDisplayName:      req.AppDisplayName,
		PrivateKeySecretRef: req.PrivateKeySecretRef,
		WebhookSecretRef:    req.WebhookSecretRef,
		InstallationID:      req.InstallationID,
		AccountLogin:        req.AccountLogin,
		AccountType:         req.AccountType,
		AccountID:           req.AccountID,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toSCMConnectionResponse(connection))
}

func (h *SCMHandler) GetConnection(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	connection, err := h.admin.GetConnection(r.Context(), strings.TrimSpace(chi.URLParam(r, "connectionID")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toSCMConnectionResponse(connection))
}

func (h *SCMHandler) PatchConnection(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.PatchSCMConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	connection, err := h.admin.SetConnectionEnabled(r.Context(), strings.TrimSpace(chi.URLParam(r, "connectionID")), req.Enabled)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toSCMConnectionResponse(connection))
}

func (h *SCMHandler) ListRegisteredRepositories(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	repositories, err := h.admin.ListRegisteredRepositories(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	responses := make([]api.SCMRepositoryRegistrationResponse, 0, len(repositories))
	for _, item := range repositories {
		responses = append(responses, toSCMRepositoryRegistrationResponse(item))
	}
	writeDataJSON(w, http.StatusOK, api.SCMRepositoryRegistrationListResponse{Repositories: responses})
}

func (h *SCMHandler) CreateRegisteredRepository(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.CreateSCMRepositoryRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	archived := req.Archived != nil && *req.Archived
	disabled := req.Disabled != nil && *req.Disabled
	repositoryRegistration, err := h.admin.CreateRegisteredRepository(r.Context(), service.CreateSCMRepositoryRegistrationInput{
		ConnectionID:         req.ConnectionID,
		ProviderRepositoryID: req.ProviderRepositoryID,
		Owner:                req.Owner,
		Name:                 req.Name,
		FullName:             req.FullName,
		CloneURL:             req.CloneURL,
		WebURL:               req.WebURL,
		DefaultBranch:        req.DefaultBranch,
		Archived:             archived,
		Disabled:             disabled,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toSCMRepositoryRegistrationResponse(repositoryRegistration))
}

func (h *SCMHandler) GetRegisteredRepository(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	repositoryRegistration, err := h.admin.GetRegisteredRepository(r.Context(), strings.TrimSpace(chi.URLParam(r, "repositoryID")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toSCMRepositoryRegistrationResponse(repositoryRegistration))
}

func (h *SCMHandler) authorizeAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h == nil || h.admin == nil {
		writeErrorJSON(w, http.StatusNotFound, "not_found", "scm endpoint is not available")
		return false
	}
	return authorizeGlobalAdmin(w, r, h.authMode, "global admin is required")
}

func (h *SCMHandler) writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, repository.ErrSCMConnectionNotFound) || errors.Is(err, repository.ErrSCMRepositoryRegistrationNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, repository.ErrSCMConnectionConflict) || errors.Is(err, repository.ErrSCMGitHubAppRegistrationConflict) || errors.Is(err, repository.ErrSCMGitHubAppInstallationConflict) || errors.Is(err, repository.ErrSCMRepositoryRegistrationDuplicate) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMConnectionDisplayNameRequired) ||
		errors.Is(err, service.ErrSCMConnectionDeploymentKindInvalid) ||
		errors.Is(err, service.ErrSCMGitHubAppIDRequired) ||
		errors.Is(err, service.ErrSCMGitHubPrivateKeySecretRefRequired) ||
		errors.Is(err, service.ErrSCMGitHubWebhookSecretRefRequired) ||
		errors.Is(err, service.ErrSCMGitHubInstallationIDRequired) ||
		errors.Is(err, service.ErrSCMGitHubAccountLoginRequired) ||
		errors.Is(err, service.ErrSCMGitHubAccountTypeRequired) ||
		errors.Is(err, service.ErrSCMGitHubAccountIDRequired) ||
		errors.Is(err, service.ErrSCMConnectionEnabledRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryConnectionIDRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryProviderRepositoryIDRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryOwnerRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryNameRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryFullNameRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryCloneURLRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryWebURLRequired) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
}

func toSCMConnectionResponse(detail domain.SCMConnectionDetail) api.SCMConnectionResponse {
	response := api.SCMConnectionResponse{
		ID:             detail.Connection.ID,
		Provider:       string(detail.Connection.Provider),
		DisplayName:    detail.Connection.DisplayName,
		DeploymentKind: string(detail.Connection.DeploymentKind),
		APIBaseURL:     detail.Connection.APIBaseURL,
		WebBaseURL:     detail.Connection.WebBaseURL,
		Enabled:        detail.Connection.Enabled,
		HealthStatus:   string(detail.Connection.HealthStatus),
		HealthSummary:  detail.Connection.HealthSummary,
		CreatedAt:      detail.Connection.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      detail.Connection.UpdatedAt.Format(time.RFC3339),
	}
	if detail.Connection.LastHealthCheckedAt != nil {
		formatted := detail.Connection.LastHealthCheckedAt.Format(time.RFC3339)
		response.LastHealthCheckedAt = &formatted
	}
	if detail.GitHubAppRegistration != nil {
		response.GitHubApp = &api.GitHubAppRegistrationResponse{
			ID:                  detail.GitHubAppRegistration.ID,
			AppID:               detail.GitHubAppRegistration.AppID,
			DisplayName:         detail.GitHubAppRegistration.DisplayName,
			APIBaseURL:          detail.GitHubAppRegistration.APIBaseURL,
			WebBaseURL:          detail.GitHubAppRegistration.WebBaseURL,
			PrivateKeySecretRef: detail.GitHubAppRegistration.PrivateKeySecretRef,
			WebhookSecretRef:    detail.GitHubAppRegistration.WebhookSecretRef,
			CreatedAt:           detail.GitHubAppRegistration.CreatedAt.Format(time.RFC3339),
			UpdatedAt:           detail.GitHubAppRegistration.UpdatedAt.Format(time.RFC3339),
		}
	}
	if detail.GitHubAppInstallation != nil {
		response.GitHubInstallation = &api.GitHubAppInstallationResponse{
			ConnectionID:      detail.GitHubAppInstallation.ConnectionID,
			AppRegistrationID: detail.GitHubAppInstallation.AppRegistrationID,
			InstallationID:    detail.GitHubAppInstallation.InstallationID,
			AccountLogin:      detail.GitHubAppInstallation.AccountLogin,
			AccountType:       detail.GitHubAppInstallation.AccountType,
			AccountID:         detail.GitHubAppInstallation.AccountID,
			CreatedAt:         detail.GitHubAppInstallation.CreatedAt.Format(time.RFC3339),
			UpdatedAt:         detail.GitHubAppInstallation.UpdatedAt.Format(time.RFC3339),
		}
	}
	return response
}

func toSCMRepositoryRegistrationResponse(registration domain.SCMRepositoryRegistration) api.SCMRepositoryRegistrationResponse {
	return api.SCMRepositoryRegistrationResponse{
		ID:                   registration.ID,
		ConnectionID:         registration.ConnectionID,
		ProviderRepositoryID: registration.ProviderRepositoryID,
		Owner:                registration.Owner,
		Name:                 registration.Name,
		FullName:             registration.FullName,
		CloneURL:             registration.CloneURL,
		WebURL:               registration.WebURL,
		DefaultBranch:        registration.DefaultBranch,
		Archived:             registration.Archived,
		Disabled:             registration.Disabled,
		MetadataRefreshedAt:  registration.MetadataRefreshedAt.Format(time.RFC3339),
		CreatedAt:            registration.CreatedAt.Format(time.RFC3339),
		UpdatedAt:            registration.UpdatedAt.Format(time.RFC3339),
	}
}
