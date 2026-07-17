package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrSCMConnectionNotFound = errors.New("scm connection not found")
var ErrSCMConnectionConflict = errors.New("scm connection conflict")
var ErrSCMGitHubAppRegistrationNotFound = errors.New("github app registration not found")
var ErrSCMGitHubAppRegistrationConflict = errors.New("github app registration conflict")
var ErrSCMGitHubAppInstallationConflict = errors.New("github app installation conflict")

type SCMConnectionRepository interface {
	CreateGitHubAppRegistration(ctx context.Context, registration domain.GitHubAppRegistration) (domain.GitHubAppRegistration, error)
	ListGitHubAppRegistrations(ctx context.Context) ([]domain.GitHubAppRegistration, error)
	GetGitHubAppRegistrationByID(ctx context.Context, id string) (domain.GitHubAppRegistration, error)
	CreateGitHubAppInstallationConnection(ctx context.Context, detail domain.SCMConnectionDetail) (domain.SCMConnectionDetail, error)
	List(ctx context.Context) ([]domain.SCMConnectionDetail, error)
	GetByID(ctx context.Context, id string) (domain.SCMConnectionDetail, error)
	SetEnabled(ctx context.Context, id string, enabled bool, updatedAt time.Time) (domain.SCMConnectionDetail, error)
}
