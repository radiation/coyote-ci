package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

var ErrProjectMembershipRoleInvalid = errors.New("membership role must be one of owner, maintainer, viewer")

type ProjectMembershipService struct {
	projects    repository.ProjectRepository
	memberships repository.ProjectMembershipRepository
	now         func() time.Time
}

func NewProjectMembershipService(projects repository.ProjectRepository, memberships repository.ProjectMembershipRepository) *ProjectMembershipService {
	return &ProjectMembershipService{projects: projects, memberships: memberships, now: time.Now}
}

type UpsertProjectMembershipInput struct {
	ProjectID string
	UserID    string
	Role      string
}

func (s *ProjectMembershipService) UpsertProjectMembership(ctx context.Context, input UpsertProjectMembershipInput) (domain.ProjectMembership, error) {
	projectID := strings.TrimSpace(input.ProjectID)
	userID := strings.TrimSpace(input.UserID)
	if projectID == "" {
		return domain.ProjectMembership{}, repository.ErrProjectNotFound
	}
	if userID == "" {
		return domain.ProjectMembership{}, repository.ErrUserNotFound
	}
	role, err := normalizeProjectMemberRole(input.Role)
	if err != nil {
		return domain.ProjectMembership{}, err
	}

	now := s.now().UTC()
	return s.memberships.Upsert(ctx, domain.ProjectMembership{
		ProjectID: projectID,
		UserID:    userID,
		Role:      role,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

func (s *ProjectMembershipService) GetProjectMembership(ctx context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	return s.memberships.Get(ctx, strings.TrimSpace(projectID), strings.TrimSpace(userID))
}

func (s *ProjectMembershipService) ListProjectMembershipsByUser(ctx context.Context, userID string) ([]domain.ProjectMembership, error) {
	trimmedUserID := strings.TrimSpace(userID)
	if trimmedUserID == "" {
		return []domain.ProjectMembership{}, nil
	}
	return s.memberships.ListByUserID(ctx, trimmedUserID)
}

func (s *ProjectMembershipService) ListProjectMembers(ctx context.Context, projectID string) ([]domain.ProjectMembershipWithUser, error) {
	trimmedProjectID := strings.TrimSpace(projectID)
	if trimmedProjectID == "" {
		return nil, repository.ErrProjectNotFound
	}
	if s.projects != nil {
		if _, err := s.projects.GetByID(ctx, trimmedProjectID); err != nil {
			return nil, err
		}
	}
	return s.memberships.ListByProjectID(ctx, trimmedProjectID)
}

func (s *ProjectMembershipService) DeleteProjectMembership(ctx context.Context, projectID string, userID string) error {
	trimmedProjectID := strings.TrimSpace(projectID)
	trimmedUserID := strings.TrimSpace(userID)
	if trimmedProjectID == "" {
		return repository.ErrProjectNotFound
	}
	if trimmedUserID == "" {
		return repository.ErrUserNotFound
	}
	return s.memberships.Delete(ctx, trimmedProjectID, trimmedUserID)
}

func normalizeProjectMemberRole(value string) (domain.ProjectMemberRole, error) {
	switch domain.ProjectMemberRole(strings.TrimSpace(value)) {
	case domain.ProjectMemberRoleOwner, domain.ProjectMemberRoleMaintainer, domain.ProjectMemberRoleViewer:
		return domain.ProjectMemberRole(strings.TrimSpace(value)), nil
	default:
		return "", ErrProjectMembershipRoleInvalid
	}
}
