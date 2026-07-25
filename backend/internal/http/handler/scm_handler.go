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
	ListGitHubAppRegistrations(ctx context.Context) ([]domain.GitHubAppRegistration, error)
	GetGitHubAppRegistration(ctx context.Context, id string) (domain.GitHubAppRegistration, error)
	CreateGitHubAppRegistration(ctx context.Context, input service.CreateGitHubAppRegistrationInput) (domain.GitHubAppRegistration, error)
	CreateGitHubAppInstallationConnection(ctx context.Context, input service.CreateGitHubAppInstallationConnectionInput) (domain.SCMConnectionDetail, error)
	SetConnectionEnabled(ctx context.Context, id string, enabled *bool) (domain.SCMConnectionDetail, error)
	TestConnection(ctx context.Context, id string) (domain.SCMConnectionDetail, error)
	ListRegisteredRepositories(ctx context.Context) ([]domain.SCMRepositoryRegistration, error)
	GetRegisteredRepository(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error)
	CreateRegisteredRepository(ctx context.Context, input service.CreateSCMRepositoryRegistrationInput) (domain.SCMRepositoryRegistration, error)
	RefreshRegisteredRepository(ctx context.Context, id string) (domain.SCMRepositoryRegistration, error)
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

// ListGitHubAppRegistrations godoc
// @Summary List GitHub App registrations
// @Description Returns safe GitHub App registration metadata for operator rediscovery.
// @Tags scm
// @Produce json
// @Success 200 {object} api.GitHubAppRegistrationListEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/scm/github-apps [get]
func (h *SCMHandler) ListGitHubAppRegistrations(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	registrations, err := h.admin.ListGitHubAppRegistrations(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	responses := make([]api.GitHubAppRegistrationResponse, 0, len(registrations))
	for _, item := range registrations {
		responses = append(responses, toGitHubAppRegistrationResponse(item))
	}
	writeDataJSON(w, http.StatusOK, api.GitHubAppRegistrationListResponse{GitHubApps: responses})
}

// GetGitHubAppRegistration godoc
// @Summary Get GitHub App registration
// @Description Returns safe GitHub App registration metadata by ID.
// @Tags scm
// @Produce json
// @Success 200 {object} api.GitHubAppRegistrationEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/scm/github-apps/{registrationID} [get]
func (h *SCMHandler) GetGitHubAppRegistration(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	registration, err := h.admin.GetGitHubAppRegistration(r.Context(), strings.TrimSpace(chi.URLParam(r, "registrationID")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toGitHubAppRegistrationResponse(registration))
}

// CreateGitHubAppRegistration godoc
// @Summary Create GitHub App registration
// @Description Creates reusable GitHub App registration metadata and secret references without creating an installation-backed SCM connection.
// @Tags scm
// @Accept json
// @Produce json
// @Param request body api.CreateGitHubAppRegistrationRequest true "GitHub App registration request"
// @Success 201 {object} api.GitHubAppRegistrationEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/scm/github-apps [post]
func (h *SCMHandler) CreateGitHubAppRegistration(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	var req api.CreateGitHubAppRegistrationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	registration, err := h.admin.CreateGitHubAppRegistration(r.Context(), service.CreateGitHubAppRegistrationInput{
		AppID:               req.AppID,
		DisplayName:         req.DisplayName,
		APIBaseURL:          req.APIBaseURL,
		WebBaseURL:          req.WebBaseURL,
		PrivateKeySecretRef: req.PrivateKeySecretRef,
		WebhookSecretRef:    req.WebhookSecretRef,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toGitHubAppRegistrationResponse(registration))
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

// CreateGitHubAppInstallationConnection godoc
// @Summary Create installation-backed SCM connection
// @Description Creates a GitHub App installation-backed SCM connection that references an existing GitHub App registration.
// @Tags scm
// @Accept json
// @Produce json
// @Param request body api.CreateGitHubAppInstallationConnectionRequest true "GitHub App installation connection request"
// @Success 201 {object} api.SCMConnectionEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 409 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/scm/connections/github-app-installations [post]
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
		AppRegistrationID: req.AppRegistrationID,
		DisplayName:       req.DisplayName,
		Enabled:           req.Enabled,
		InstallationID:    req.InstallationID,
		AccountLogin:      req.AccountLogin,
		AccountType:       req.AccountType,
		TargetID:          req.TargetID,
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

// TestConnection godoc
// @Summary Test installation-backed SCM connection
// @Description Verifies GitHub App installation authentication for one SCM connection and updates bounded health metadata.
// @Tags scm
// @Produce json
// @Success 200 {object} api.SCMConnectionEnvelope
// @Failure 400 {object} api.ErrorResponse
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 502 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/scm/connections/{connectionID}/test [post]
func (h *SCMHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	connection, err := h.admin.TestConnection(r.Context(), strings.TrimSpace(chi.URLParam(r, "connectionID")))
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
	repositoryRegistration, err := h.admin.CreateRegisteredRepository(r.Context(), service.CreateSCMRepositoryRegistrationInput{
		ConnectionID:         req.ConnectionID,
		ProviderRepositoryID: req.ProviderRepositoryID,
		Owner:                req.Owner,
		Name:                 req.Name,
	})
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusCreated, toSCMRepositoryRegistrationResponse(repositoryRegistration))
}

// RefreshRegisteredRepository godoc
// @Summary Refresh SCM repository metadata
// @Description Refreshes repository metadata from the provider using the stored connection and provider repository ID.
// @Tags scm
// @Produce json
// @Success 200 {object} api.SCMRepositoryRegistrationEnvelope
// @Failure 401 {object} api.ErrorResponse
// @Failure 403 {object} api.ErrorResponse
// @Failure 404 {object} api.ErrorResponse
// @Failure 502 {object} api.ErrorResponse
// @Failure 500 {object} api.ErrorResponse
// @Router /settings/scm/repositories/{repositoryID}/refresh [post]
func (h *SCMHandler) RefreshRegisteredRepository(w http.ResponseWriter, r *http.Request) {
	if !h.authorizeAdmin(w, r) {
		return
	}
	repositoryRegistration, err := h.admin.RefreshRegisteredRepository(r.Context(), strings.TrimSpace(chi.URLParam(r, "repositoryID")))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeDataJSON(w, http.StatusOK, toSCMRepositoryRegistrationResponse(repositoryRegistration))
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
	if errors.Is(err, repository.ErrSCMConnectionNotFound) || errors.Is(err, repository.ErrSCMGitHubAppRegistrationNotFound) || errors.Is(err, repository.ErrSCMRepositoryRegistrationNotFound) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, repository.ErrSCMConnectionConflict) || errors.Is(err, repository.ErrSCMGitHubAppRegistrationConflict) || errors.Is(err, repository.ErrSCMGitHubAppInstallationConflict) || errors.Is(err, repository.ErrSCMRepositoryRegistrationDuplicate) {
		writeErrorJSON(w, http.StatusConflict, "conflict", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMConnectionDisplayNameRequired) ||
		errors.Is(err, service.ErrSCMGitHubAppRegistrationIDRequired) ||
		errors.Is(err, service.ErrSCMGitHubAppIDRequired) ||
		errors.Is(err, service.ErrSCMGitHubPrivateKeySecretRefRequired) ||
		errors.Is(err, service.ErrSCMGitHubWebhookSecretRefRequired) ||
		errors.Is(err, service.ErrSCMGitHubInstallationIDRequired) ||
		errors.Is(err, service.ErrSCMGitHubAccountLoginRequired) ||
		errors.Is(err, service.ErrSCMGitHubAccountTypeRequired) ||
		errors.Is(err, service.ErrSCMGitHubTargetIDRequired) ||
		errors.Is(err, service.ErrSCMConnectionEnabledRequired) ||
		errors.Is(err, service.ErrSCMConnectionDisabled) ||
		errors.Is(err, service.ErrSCMGitHubConnectionConfigurationInvalid) ||
		errors.Is(err, service.ErrSCMRegisteredRepositoryConnectionIDRequired) ||
		errors.Is(err, service.ErrSCMRegisteredRepositorySelectorInvalid) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMGitHubRepositoryNotAccessible) {
		writeErrorJSON(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMGitHubPrivateKeyResolveFailed) {
		writeErrorJSON(w, http.StatusBadGateway, "secret_resolution_failed", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMGitHubAuthenticationFailed) {
		writeErrorJSON(w, http.StatusBadGateway, "provider_auth_failed", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMGitHubInstallationUnavailable) {
		writeErrorJSON(w, http.StatusBadGateway, "installation_unavailable", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMGitHubRateLimited) {
		writeErrorJSON(w, http.StatusBadGateway, "rate_limited", err.Error())
		return
	}
	if errors.Is(err, service.ErrSCMGitHubProviderUnavailable) || errors.Is(err, service.ErrSCMGitHubProviderMalformedResponse) {
		writeErrorJSON(w, http.StatusBadGateway, "provider_unavailable", err.Error())
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
		githubApp := toGitHubAppRegistrationResponse(*detail.GitHubAppRegistration)
		response.GitHubApp = &githubApp
	}
	if detail.GitHubAppInstallation != nil {
		response.GitHubInstallation = &api.GitHubAppInstallationResponse{
			ConnectionID:      detail.GitHubAppInstallation.ConnectionID,
			AppRegistrationID: detail.GitHubAppInstallation.AppRegistrationID,
			InstallationID:    detail.GitHubAppInstallation.InstallationID,
			AccountLogin:      detail.GitHubAppInstallation.AccountLogin,
			AccountType:       detail.GitHubAppInstallation.AccountType,
			TargetID:          detail.GitHubAppInstallation.AccountID,
			CreatedAt:         detail.GitHubAppInstallation.CreatedAt.Format(time.RFC3339),
			UpdatedAt:         detail.GitHubAppInstallation.UpdatedAt.Format(time.RFC3339),
		}
	}
	return response
}

func toGitHubAppRegistrationResponse(registration domain.GitHubAppRegistration) api.GitHubAppRegistrationResponse {
	return api.GitHubAppRegistrationResponse{
		ID:                   registration.ID,
		AppID:                registration.AppID,
		DisplayName:          registration.DisplayName,
		APIBaseURL:           registration.APIBaseURL,
		WebBaseURL:           registration.WebBaseURL,
		PrivateKeyConfigured: strings.TrimSpace(registration.PrivateKeySecretRef) != "",
		WebhookConfigured:    strings.TrimSpace(registration.WebhookSecretRef) != "",
		CreatedAt:            registration.CreatedAt.Format(time.RFC3339),
		UpdatedAt:            registration.UpdatedAt.Format(time.RFC3339),
	}
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
