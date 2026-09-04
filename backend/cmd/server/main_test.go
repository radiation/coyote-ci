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
	"github.com/radiation/coyote-ci/backend/internal/service"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

type stubProjectRepository struct {
	project domain.Project
}

func TestNewWorkspaceHelperHandlerIsControlledByHelperConfiguration(t *testing.T) {
	workspaceHelperHandler, handlerErr := newWorkspaceHelperHandler(config.Config{ExecutionBackend: "docker"}, nil)
	if handlerErr != nil {
		t.Fatalf("new handler: %v", handlerErr)
	}
	if workspaceHelperHandler != nil {
		t.Fatal("expected no workspace helper handler when helper configuration is disabled")
	}

	workspaceHelperHandler, handlerErr = newWorkspaceHelperHandlerWithVerifier(config.Config{
		ExecutionBackend:                 "docker",
		WorkspaceHelperCapabilityEnabled: true,
		WorkspaceHelperKubeconfig:        "/server/kubeconfig",
		WorkspaceHelperServiceAccount:    "workspace-helper",
		WorkspaceHelperCapabilitySecret:  strings.Repeat("a", 32),
	}, memoryrepo.NewExecutionJobRepository(), func(kubeconfig string, serviceAccount string) (service.WorkloadIdentityVerifier, error) {
		if kubeconfig != "/server/kubeconfig" || serviceAccount != "workspace-helper" {
			t.Fatalf("verifier config kubeconfig=%q serviceAccount=%q", kubeconfig, serviceAccount)
		}
		return &workspaceHelperIdentityVerifier{}, nil
	})
	if handlerErr != nil {
		t.Fatalf("new enabled handler: %v", handlerErr)
	}
	if workspaceHelperHandler == nil {
		t.Fatal("expected helper handler with Docker server execution backend")
	}
}

func TestWorkspaceHelperCompositionFailuresAndRevisionStoreSelection(t *testing.T) {
	verifierErr := errors.New("verifier unavailable")
	_, handlerErr := newWorkspaceHelperHandlerWithVerifier(config.Config{WorkspaceHelperCapabilityEnabled: true}, memoryrepo.NewExecutionJobRepository(), func(string, string) (service.WorkloadIdentityVerifier, error) {
		return nil, verifierErr
	})
	if !errors.Is(handlerErr, verifierErr) {
		t.Fatalf("verifier error=%v", handlerErr)
	}
	_, handlerErr = newWorkspaceHelperHandlerWithVerifier(config.Config{WorkspaceHelperCapabilityEnabled: true, WorkspaceHelperCapabilitySecret: "short"}, memoryrepo.NewExecutionJobRepository(), func(string, string) (service.WorkloadIdentityVerifier, error) {
		return &workspaceHelperIdentityVerifier{}, nil
	})
	if handlerErr == nil {
		t.Fatal("expected invalid capability secret error")
	}
	if store := workspaceRevisionStoreFromConfig(config.Config{}); store != nil {
		t.Fatalf("store=%T, want nil", store)
	}
	if store := workspaceRevisionStoreFromConfig(config.Config{WorkspaceRevisionStorageRoot: t.TempDir()}); store == nil {
		t.Fatal("expected filesystem workspace revision store")
	} else if _, ok := store.(*workspacepkg.FilesystemWorkspaceRevisionStore); !ok {
		t.Fatalf("store=%T", store)
	}
}

func TestConfigureWorkspaceHelperServices(t *testing.T) {
	executionJobs := memoryrepo.NewExecutionJobRepository()
	revisions := memoryrepo.NewWorkspaceRevisionRepository(executionJobs)
	workspaceHelperHandler, handlerErr := newWorkspaceHelperHandlerWithVerifier(config.Config{WorkspaceHelperCapabilityEnabled: true, WorkspaceHelperCapabilitySecret: strings.Repeat("a", 32)}, executionJobs, func(string, string) (service.WorkloadIdentityVerifier, error) {
		return &workspaceHelperIdentityVerifier{}, nil
	})
	if handlerErr != nil {
		t.Fatalf("new handler: %v", handlerErr)
	}
	if configureErr := configureWorkspaceHelperServices(config.Config{}, nil, executionJobs, memoryrepo.NewBuildRepository(), revisions, nil); configureErr != nil {
		t.Fatalf("disabled helper services: %v", configureErr)
	}
	if configureErr := configureWorkspaceHelperServices(config.Config{}, workspaceHelperHandler, executionJobs, memoryrepo.NewBuildRepository(), revisions, nil); configureErr == nil {
		t.Fatal("expected missing workspace storage error")
	}
	if configureErr := configureWorkspaceHelperServices(config.Config{WorkspaceRevisionStorageRoot: t.TempDir(), WorkspaceHelperMaxUploadSizeMB: 1}, workspaceHelperHandler, executionJobs, memoryrepo.NewBuildRepository(), revisions, nil); configureErr != nil {
		t.Fatalf("configure helper services: %v", configureErr)
	}
}

type workspaceHelperIdentityVerifier struct{}

func (*workspaceHelperIdentityVerifier) VerifyWorkspaceHelper(context.Context, string, string, string, domain.WorkspaceHelperRole) (service.VerifiedWorkloadIdentity, error) {
	return service.VerifiedWorkloadIdentity{}, nil
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
	originalHostname := osServerHostname
	t.Cleanup(func() {
		osServerHostname = originalHostname
	})
	osServerHostname = func() (string, error) { return "server-test", nil }

	enabled, enabledErr := configureSCMStatusRuntime(config.Config{
		PublicURL:                  "https://ci.example.com",
		SCMStatusRecoveryInterval:  time.Second,
		SCMStatusRecoveryBatchSize: 5,
	}, memoryrepo.NewBuildRepository(), &stubProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, memoryrepo.NewSCMStatusDeliveryRepository(), memoryrepo.NewSCMConnectionRepository(), memoryrepo.NewSCMRepositoryRegistrationRepository())
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
		SCMStatusRecoveryInterval:  0,
		SCMStatusRecoveryBatchSize: 0,
	}, memoryrepo.NewBuildRepository(), &stubProjectRepository{project: domain.Project{ID: "project-1", Slug: "payments"}}, memoryrepo.NewSCMStatusDeliveryRepository(), memoryrepo.NewSCMConnectionRepository(), memoryrepo.NewSCMRepositoryRegistrationRepository())
	if invalidErr == nil {
		t.Fatal("expected invalid recovery configuration to error")
	}
	if !strings.Contains(invalidErr.Error(), "interval") && !strings.Contains(invalidErr.Error(), "batch size") {
		t.Fatalf("expected recovery validation error, got %v", invalidErr)
	}

	t.Run("reporter dependency errors bubble up", func(t *testing.T) {
		cfg := config.Config{
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
				if _, err := configureSCMStatusRuntime(cfg, test.buildRepo, test.projectRepo, test.deliveryRepo, memoryrepo.NewSCMConnectionRepository(), memoryrepo.NewSCMRepositoryRegistrationRepository()); err == nil {
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

func TestNewRepositoryAwareCheckoutResolver(t *testing.T) {
	resolver, err := newRepositoryAwareCheckoutResolver(memoryrepo.NewSCMConnectionRepository(), memoryrepo.NewSCMRepositoryRegistrationRepository())
	if err != nil || resolver == nil {
		t.Fatalf("expected configured checkout resolver, resolver=%v err=%v", resolver, err)
	}
}

func TestBootstrapAdminsAtStartup(t *testing.T) {
	userService := service.NewUserService(memoryrepo.NewUserRepository())
	if err := bootstrapAdminsAtStartup(context.Background(), userService, map[string]struct{}{"ADMIN@example.com": {}}); err != nil {
		t.Fatalf("bootstrap startup provisioning failed: %v", err)
	}

	user, err := userService.ResolveOIDCUser(context.Background(), "admin@example.com", nil)
	if err != nil {
		t.Fatalf("resolve bootstrapped admin failed: %v", err)
	}
	if user.GlobalRole != domain.GlobalRoleAdmin {
		t.Fatalf("expected bootstrapped admin role, got %q", user.GlobalRole)
	}
}
