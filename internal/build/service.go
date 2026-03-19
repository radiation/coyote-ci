package build

import (
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	mu     sync.Mutex
	builds map[string]*Build
	steps  map[string][]*Step
}

func NewService() *Service {
	return &Service{
		builds: make(map[string]*Build),
		steps:  make(map[string][]*Step),
	}
}

func (s *Service) CreateBuild(repo, sha string, stepSpecs []StepSpec) *Build {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := &Build{
		ID:        uuid.NewString(),
		Repo:      repo,
		CommitSHA: sha,
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}

	s.builds[b.ID] = b

	var steps []*Step

	for _, spec := range stepSpecs {
		step := &Step{
			ID:        uuid.NewString(),
			BuildID:   b.ID,
			Name:      spec.Name,
			Command:   spec.Command,
			Status:    StatusPending,
			CreatedAt: time.Now(),
		}

		steps = append(steps, step)
	}

	s.steps[b.ID] = steps

	return b
}

func (s *Service) ListBuilds() []*Build {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*Build, 0, len(s.builds))
	for _, b := range s.builds {
		result = append(result, b)
	}
	return result
}

func (s *Service) runNextBuild() {
	s.mu.Lock()

	var next *Build
	for _, b := range s.builds {
		if b.Status == StatusPending {
			b.Status = StatusRunning
			next = b
			break
		}
	}

	s.mu.Unlock()

	if next == nil {
		return
	}

	s.executeBuild(next)
}

func (s *Service) executeBuild(b *Build) {
	s.mu.Lock()
	steps := s.steps[b.ID]
	s.mu.Unlock()

	for _, step := range steps {
		s.mu.Lock()
		step.Status = StatusRunning
		s.mu.Unlock()

		cmd := exec.Command("sh", "-c", step.Command)
		output, err := cmd.CombinedOutput()

		s.mu.Lock()
		step.Output = string(output)
		if err != nil {
			step.Status = StatusFailed
			b.Status = StatusFailed
			s.mu.Unlock()
			return
		}
		step.Status = StatusSuccess
		s.mu.Unlock()
	}

	s.mu.Lock()
	b.Status = StatusSuccess
	s.mu.Unlock()
}

func (s *Service) RunWorker() {
	for {
		s.runNextBuild()
		time.Sleep(1 * time.Second)
	}
}

func (s *Service) ListSteps(buildID string) []*Step {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.steps[buildID]
}
