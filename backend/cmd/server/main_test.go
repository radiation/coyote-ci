package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/platform/config"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	memoryrepo "github.com/radiation/coyote-ci/backend/internal/repository/memory"
)

type stubProjectRepository struct {
	project domain.Project
}

func (r *stubProjectRepository) Create(context.Context, domain.Project) (domain.Project, error) {
	return domain.Project{}, errors.New("not implemented")
}

func (r *stubProjectRepository) GetByID(context.Context, string) (domain.Project, error) {
	return r.project, nil
}

func (r *stubProjectRepository) GetByIDs(context.Context, []string) ([]domain.Project, error) {
	return []domain.Project{r.project}, nil
}

func (r *stubProjectRepository) GetBySlug(context.Context, string) (domain.Project, error) {
	return r.project, nil
}

func (r *stubProjectRepository) List(context.Context) ([]domain.Project, error) {
	return []domain.Project{r.project}, nil
}

func (r *stubProjectRepository) Update(context.Context, domain.Project) (domain.Project, error) {
	return domain.Project{}, errors.New("not implemented")
}

func (r *stubProjectRepository) Delete(context.Context, string) error {
	return errors.New("not implemented")
}

func TestConfigureSCMStatusRuntime(t *testing.T) {
	disabled, disabledErr := configureSCMStatusRuntime(config.Config{}, memoryrepo.NewBuildRepository(), &stubProjectRepository{}, memoryrepo.NewSCMStatusDeliveryRepository())
	if disabledErr != nil {
		t.Fatalf("disabled runtime should not error: %v", disabledErr)
	}
	if disabled.reporter != nil || disabled.reporterImpl != nil || disabled.recoveryDrain != nil {
		t.Fatalf("expected disabled runtime to keep nil dependencies, got %+v", disabled)
	}

	originalHostname := osServerHostname
	t.Cleanup(func() {
		osServerHostname = originalHostname
	})
	osServerHostname = func() (string, error) { return "server-test", nil }

	enabled, enabledErr := configureSCMStatusRuntime(config.Config{
		GitHubStatusToken:          " token ",
		PublicURL:                  "https://ci.example.com",
		SCMStatusRecoveryInterval:  time.Second,
		SCMStatusRecoveryBatchSize: 5,
	}, memoryrepo.NewBuildRepository(), &stubProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, memoryrepo.NewSCMStatusDeliveryRepository())
	if enabledErr != nil {
		t.Fatalf("enabled runtime should not error: %v", enabledErr)
	}
	if enabled.reporter == nil || enabled.reporterImpl == nil || enabled.recoveryDrain == nil {
		t.Fatalf("expected enabled runtime dependencies, got %+v", enabled)
	}
	if enabled.reporter == nil {
		t.Fatal("expected interface reporter to be non-nil")
	}
	if _, ok := enabled.reporter.(interface {
		NotifyBuildStatus(context.Context, domain.Build) error
	}); !ok {
		t.Fatal("expected configured reporter to satisfy build status reporter interface")
	}
	if enabled.recoveryDrain == nil {
		t.Fatal("expected recovery drain to be configured")
	}

	_, invalidErr := configureSCMStatusRuntime(config.Config{
		GitHubStatusToken:          "token",
		SCMStatusRecoveryInterval:  0,
		SCMStatusRecoveryBatchSize: 0,
	}, memoryrepo.NewBuildRepository(), &stubProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, memoryrepo.NewSCMStatusDeliveryRepository())
	if invalidErr == nil {
		t.Fatal("expected invalid recovery configuration to error")
	}
	if !strings.Contains(invalidErr.Error(), "interval") && !strings.Contains(invalidErr.Error(), "batch size") {
		t.Fatalf("expected recovery validation error, got %v", invalidErr)
	}

	t.Run("reporter dependency errors bubble up", func(t *testing.T) {
		cfg := config.Config{
			GitHubStatusToken:          "token",
			SCMStatusRecoveryInterval:  time.Second,
			SCMStatusRecoveryBatchSize: 1,
		}
		cases := []struct {
			name         string
			buildRepo    repository.BuildRepository
			projectRepo  repository.ProjectRepository
			deliveryRepo repository.SCMStatusDeliveryRepository
		}{
			{name: "missing build repo", projectRepo: &stubProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, deliveryRepo: memoryrepo.NewSCMStatusDeliveryRepository()},
			{name: "missing project repo", buildRepo: memoryrepo.NewBuildRepository(), deliveryRepo: memoryrepo.NewSCMStatusDeliveryRepository()},
			{name: "missing delivery repo", buildRepo: memoryrepo.NewBuildRepository(), projectRepo: &stubProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}},
		}
		for _, test := range cases {
			t.Run(test.name, func(t *testing.T) {
				if _, err := configureSCMStatusRuntime(cfg, test.buildRepo, test.projectRepo, test.deliveryRepo); err == nil {
					t.Fatal("expected configureSCMStatusRuntime to return dependency error")
				}
			})
		}
	})
}

func TestDefaultServerNotificationClaimOwner(t *testing.T) {
	originalHostname := osServerHostname
	t.Cleanup(func() {
		osServerHostname = originalHostname
	})

	osServerHostname = func() (string, error) { return "ci-host", nil }
	owner := defaultServerNotificationClaimOwner()
	if !strings.HasPrefix(owner, "server-ci-host-") {
		t.Fatalf("expected owner prefix with hostname, got %q", owner)
	}

	osServerHostname = func() (string, error) { return "  ", nil }
	fallbackOwner := defaultServerNotificationClaimOwner()
	if !strings.HasPrefix(fallbackOwner, "server-unknown-host-") {
		t.Fatalf("expected fallback owner prefix, got %q", fallbackOwner)
	}

	osServerHostname = func() (string, error) { return "", repository.ErrProjectNotFound }
	errorOwner := defaultServerNotificationClaimOwner()
	if !strings.HasPrefix(errorOwner, "server-unknown-host-") {
		t.Fatalf("expected error fallback owner prefix, got %q", errorOwner)
	}
}
