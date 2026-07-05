package build

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/logs"
	"github.com/radiation/coyote-ci/backend/internal/repository"
)

type tailReaderSink struct {
	chunks    []logs.StepLogChunk
	truncated bool
	err       error
	buildID   string
	stepIndex *int
	limit     int
}

func (s *tailReaderSink) WriteStepLog(context.Context, string, string, string) error {
	return nil
}

func (s *tailReaderSink) ListStepLogChunksTail(_ context.Context, buildID string, stepIndex *int, limit int) ([]logs.StepLogChunk, bool, error) {
	s.buildID = buildID
	s.stepIndex = stepIndex
	s.limit = limit
	if s.err != nil {
		return nil, false, s.err
	}
	return s.chunks, s.truncated, nil
}

func TestBuildService_GetBuildLogChunksTail(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	stepIndex := 2
	sink := &tailReaderSink{chunks: []logs.StepLogChunk{{BuildID: "build-1", StepIndex: 2, ChunkText: "tail"}}, truncated: true}
	svc := NewBuildService(repo, nil, sink)

	chunks, truncated, err := svc.GetBuildLogChunksTail(context.Background(), "build-1", &stepIndex, 77)
	if err != nil {
		t.Fatalf("GetBuildLogChunksTail failed: %v", err)
	}
	if !truncated || len(chunks) != 1 || chunks[0].ChunkText != "tail" {
		t.Fatalf("unexpected tail result: chunks=%+v truncated=%v", chunks, truncated)
	}
	if sink.buildID != "build-1" || sink.stepIndex == nil || *sink.stepIndex != 2 || sink.limit != 77 {
		t.Fatalf("unexpected sink call: %+v", sink)
	}
}

func TestBuildService_GetBuildLogChunksTail_FallbackAndErrors(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeBuildRepository{build: domain.Build{ID: "build-1", ProjectID: "project-1", Status: domain.BuildStatusRunning, CreatedAt: now}}
	svc := NewBuildService(repo, nil, logs.NewNoopSink())

	chunks, truncated, err := svc.GetBuildLogChunksTail(context.Background(), "build-1", nil, 10)
	if err != nil {
		t.Fatalf("expected noop fallback, got %v", err)
	}
	if truncated || len(chunks) != 0 {
		t.Fatalf("expected empty noop fallback, got chunks=%+v truncated=%v", chunks, truncated)
	}

	notFoundRepo := &fakeBuildRepository{getErr: repository.ErrBuildNotFound}
	notFoundSvc := NewBuildService(notFoundRepo, nil, logs.NewNoopSink())
	_, _, notFoundErr := notFoundSvc.GetBuildLogChunksTail(context.Background(), "missing", nil, 10)
	if !errors.Is(notFoundErr, ErrBuildNotFound) {
		t.Fatalf("expected ErrBuildNotFound, got %v", notFoundErr)
	}

	sinkErr := errors.New("tail read failed")
	errSvc := NewBuildService(repo, nil, &tailReaderSink{err: sinkErr})
	_, _, gotErr := errSvc.GetBuildLogChunksTail(context.Background(), "build-1", nil, 10)
	if !errors.Is(gotErr, sinkErr) {
		t.Fatalf("expected sink error, got %v", gotErr)
	}
}
