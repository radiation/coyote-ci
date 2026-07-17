package repository

import (
	"context"
	"errors"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var ErrSCMConnectionNotFound = errors.New("scm connection not found")
var ErrSCMConnectionConflict = errors.New("scm connection conflict")
var ErrSCMGitHubAppRegistrationConflict = errors.New("github app registration conflict")
var ErrSCMGitHubAppInstallationConflict = errors.New("github app installation conflict")

type SCMConnectionRepository interface {
	CreateGitHubAppInstallationConnection(ctx context.Context, detail domain.SCMConnectionDetail) (domain.SCMConnectionDetail, error)
	List(ctx context.Context) ([]domain.SCMConnectionDetail, error)
	GetByID(ctx context.Context, id string) (domain.SCMConnectionDetail, error)
	SetEnabled(ctx context.Context, id string, enabled bool, updatedAt time.Time) (domain.SCMConnectionDetail, error)
}
