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
}

func NewService() *Service {
	return &Service{
		builds: make(map[string]*Build),
	}
}

func (s *Service) CreateBuild(repo, sha, command string) *Build {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := &Build{
		ID:        uuid.NewString(),
		Repo:      repo,
		CommitSHA: sha,
		Command:   command,
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}

	s.builds[b.ID] = b
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
	cmd := exec.Command("sh", "-c", b.Command)

	output, err := cmd.CombinedOutput()

	s.mu.Lock()
	defer s.mu.Unlock()

	if err != nil {
		b.Status = StatusFailed
	} else {
		b.Status = StatusSuccess
	}

	// Stub: I'll add logging and artifact storage later
	_ = output
}

func (s *Service) RunWorker() {
	for {
		s.runNextBuild()
		time.Sleep(1 * time.Second)
	}
}
