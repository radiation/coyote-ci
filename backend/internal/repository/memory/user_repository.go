package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type UserRepository struct {
	mu          sync.RWMutex
	users       map[string]domain.User
	memberships *ProjectMembershipRepository
}

func NewUserRepository() *UserRepository {
	return &UserRepository{users: map[string]domain.User{}}
}

func (r *UserRepository) Create(_ context.Context, user domain.User) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	if r.emailExistsLocked(user.Email, "") {
		return domain.User{}, repository.ErrUserEmailConflict
	}
	r.users[user.ID] = user
	return user, nil
}

func (r *UserRepository) GetByID(_ context.Context, id string) (domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, ok := r.users[id]
	if !ok {
		return domain.User{}, repository.ErrUserNotFound
	}
	return user, nil
}

func (r *UserRepository) GetByEmail(_ context.Context, email string) (domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, user := range r.users {
		if strings.EqualFold(user.Email, email) {
			return user, nil
		}
	}
	return domain.User{}, repository.ErrUserNotFound
}

func (r *UserRepository) List(_ context.Context) ([]domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]domain.User, 0, len(r.users))
	for _, user := range r.users {
		out = append(out, user)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (r *UserRepository) Update(_ context.Context, user domain.User) (domain.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.users[user.ID]; !ok {
		return domain.User{}, repository.ErrUserNotFound
	}
	if r.emailExistsLocked(user.Email, user.ID) {
		return domain.User{}, repository.ErrUserEmailConflict
	}
	r.users[user.ID] = user
	return user, nil
}

func (r *UserRepository) Delete(_ context.Context, id string) error {
	r.mu.Lock()

	if _, ok := r.users[id]; !ok {
		r.mu.Unlock()
		return repository.ErrUserNotFound
	}
	delete(r.users, id)
	memberships := r.memberships
	r.mu.Unlock()

	if memberships != nil {
		memberships.deleteByUserID(id)
	}
	return nil
}

func (r *UserRepository) emailExistsLocked(email string, excludeID string) bool {
	for id, user := range r.users {
		if id == excludeID {
			continue
		}
		if strings.EqualFold(user.Email, email) {
			return true
		}
	}
	return false
}

type ProjectMembershipRepository struct {
	mu          sync.RWMutex
	memberships map[string]domain.ProjectMembership
	projects    repository.ProjectRepository
	users       repository.UserRepository
}

func NewProjectMembershipRepository(projects repository.ProjectRepository, users repository.UserRepository) *ProjectMembershipRepository {
	repo := &ProjectMembershipRepository{
		memberships: map[string]domain.ProjectMembership{},
		projects:    projects,
		users:       users,
	}
	if userRepo, ok := users.(*UserRepository); ok {
		userRepo.memberships = repo
	}
	return repo
}

func (r *ProjectMembershipRepository) Upsert(ctx context.Context, membership domain.ProjectMembership) (domain.ProjectMembership, error) {
	if r.projects != nil {
		if _, err := r.projects.GetByID(ctx, membership.ProjectID); err != nil {
			return domain.ProjectMembership{}, err
		}
	}
	if r.users != nil {
		if _, err := r.users.GetByID(ctx, membership.UserID); err != nil {
			return domain.ProjectMembership{}, err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := membershipKey(membership.ProjectID, membership.UserID)
	if current, ok := r.memberships[key]; ok {
		membership.CreatedAt = current.CreatedAt
	}
	r.memberships[key] = membership
	return membership, nil
}

func (r *ProjectMembershipRepository) Get(_ context.Context, projectID string, userID string) (domain.ProjectMembership, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	membership, ok := r.memberships[membershipKey(projectID, userID)]
	if !ok {
		return domain.ProjectMembership{}, repository.ErrProjectMembershipNotFound
	}
	return membership, nil
}

func (r *ProjectMembershipRepository) ListByUserID(_ context.Context, userID string) ([]domain.ProjectMembership, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	memberships := make([]domain.ProjectMembership, 0)
	for _, membership := range r.memberships {
		if membership.UserID == userID {
			memberships = append(memberships, membership)
		}
	}
	sort.Slice(memberships, func(i, j int) bool {
		if memberships[i].ProjectID == memberships[j].ProjectID {
			return memberships[i].CreatedAt.Before(memberships[j].CreatedAt)
		}
		return memberships[i].ProjectID < memberships[j].ProjectID
	})
	return memberships, nil
}

func (r *ProjectMembershipRepository) ListByProjectID(ctx context.Context, projectID string) ([]domain.ProjectMembershipWithUser, error) {
	if r.projects != nil {
		if _, err := r.projects.GetByID(ctx, projectID); err != nil {
			return nil, err
		}
	}

	r.mu.RLock()
	memberships := make([]domain.ProjectMembership, 0)
	for _, membership := range r.memberships {
		if membership.ProjectID == projectID {
			memberships = append(memberships, membership)
		}
	}
	r.mu.RUnlock()

	out := make([]domain.ProjectMembershipWithUser, 0, len(memberships))
	for _, membership := range memberships {
		user, err := r.users.GetByID(ctx, membership.UserID)
		if err != nil {
			return nil, err
		}
		out = append(out, domain.ProjectMembershipWithUser{
			ProjectMembership: membership,
			Email:             user.Email,
			DisplayName:       user.DisplayName,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Email == out[j].Email {
			return out[i].UserID < out[j].UserID
		}
		return out[i].Email < out[j].Email
	})
	return out, nil
}

func (r *ProjectMembershipRepository) Delete(_ context.Context, projectID string, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := membershipKey(projectID, userID)
	if _, ok := r.memberships[key]; !ok {
		return repository.ErrProjectMembershipNotFound
	}
	delete(r.memberships, key)
	return nil
}

func (r *ProjectMembershipRepository) deleteByUserID(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for key, membership := range r.memberships {
		if membership.UserID == userID {
			delete(r.memberships, key)
		}
	}
}

func membershipKey(projectID string, userID string) string {
	return projectID + ":" + userID
}
