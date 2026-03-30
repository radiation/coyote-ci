package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// CollectedArtifact is a collected artifact payload ready for metadata persistence.
type CollectedArtifact struct {
	LogicalPath    string
	StorageKey     string
	SizeBytes      int64
	ContentType    *string
	ChecksumSHA256 *string
}

type CollectRequest struct {
	BuildID       string
	WorkspacePath string
	Patterns      []string
}

type CollectResult struct {
	Artifacts []CollectedArtifact
	Warnings  []string
}

type Collector struct {
	store Store
}

func NewCollector(store Store) *Collector {
	return &Collector{store: store}
}

func (c *Collector) Collect(ctx context.Context, request CollectRequest) (CollectResult, error) {
	if c.store == nil {
		return CollectResult{}, fmt.Errorf("artifact store is required")
	}

	buildID := strings.TrimSpace(request.BuildID)
	workspacePath := strings.TrimSpace(request.WorkspacePath)
	if buildID == "" || workspacePath == "" {
		return CollectResult{}, nil
	}

	patterns := normalizePatterns(request.Patterns)
	if len(patterns) == 0 {
		return CollectResult{}, nil
	}

	if _, err := os.Stat(workspacePath); err != nil {
		if os.IsNotExist(err) {
			return CollectResult{Warnings: []string{fmt.Sprintf("workspace %q not found; skipping artifact collection", workspacePath)}}, nil
		}
		return CollectResult{}, fmt.Errorf("checking workspace for artifact collection: %w", err)
	}

	matchedByPattern := make(map[string]bool, len(patterns))
	selected := map[string]struct{}{}

	walkErr := filepath.WalkDir(workspacePath, func(absPath string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, relErr := filepath.Rel(workspacePath, absPath)
		if relErr != nil {
			return relErr
		}
		normalizedRel := filepath.ToSlash(relPath)

		for _, pattern := range patterns {
			if matchPathPattern(pattern, normalizedRel) {
				selected[normalizedRel] = struct{}{}
				matchedByPattern[pattern] = true
				break
			}
		}

		return nil
	})
	if walkErr != nil {
		return CollectResult{}, fmt.Errorf("walking workspace for artifact collection: %w", walkErr)
	}

	warnings := make([]string, 0)
	for _, pattern := range patterns {
		if !matchedByPattern[pattern] {
			warnings = append(warnings, fmt.Sprintf("artifact pattern %q matched no files", pattern))
		}
	}

	artifacts := make([]CollectedArtifact, 0, len(selected))
	for relPath := range selected {
		artifact, err := c.collectSingle(ctx, buildID, workspacePath, relPath)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("failed collecting artifact %q: %v", relPath, err))
			continue
		}
		artifacts = append(artifacts, artifact)
	}

	return CollectResult{Artifacts: artifacts, Warnings: warnings}, nil
}

func (c *Collector) collectSingle(ctx context.Context, buildID string, workspacePath string, logicalPath string) (CollectedArtifact, error) {
	absPath := filepath.Join(workspacePath, filepath.FromSlash(logicalPath))

	file, err := os.Open(absPath)
	if err != nil {
		return CollectedArtifact{}, err
	}
	defer func() {
		_ = file.Close()
	}()

	hasher := sha256.New()
	tee := io.TeeReader(file, hasher)
	storageKey := path.Join(buildID, logicalPath)
	size, err := c.store.Save(ctx, storageKey, tee)
	if err != nil {
		return CollectedArtifact{}, err
	}

	sum := hex.EncodeToString(hasher.Sum(nil))
	checksum := &sum

	var contentType *string
	if detected := strings.TrimSpace(mime.TypeByExtension(filepath.Ext(logicalPath))); detected != "" {
		contentType = &detected
	}

	return CollectedArtifact{
		LogicalPath:    logicalPath,
		StorageKey:     storageKey,
		SizeBytes:      size,
		ContentType:    contentType,
		ChecksumSHA256: checksum,
	}, nil
}

func normalizePatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}

	normalized := make([]string, 0, len(patterns))
	seen := map[string]struct{}{}
	for _, pattern := range patterns {
		trimmed := strings.TrimSpace(pattern)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	return normalized
}

func matchPathPattern(pattern string, relPath string) bool {
	patternSegs := strings.Split(pattern, "/")
	pathSegs := strings.Split(relPath, "/")
	return matchSegments(patternSegs, pathSegs)
}

func matchSegments(patternSegs []string, pathSegs []string) bool {
	if len(patternSegs) == 0 {
		return len(pathSegs) == 0
	}

	head := patternSegs[0]
	if head == "**" {
		if len(patternSegs) == 1 {
			return true
		}
		for i := 0; i <= len(pathSegs); i++ {
			if matchSegments(patternSegs[1:], pathSegs[i:]) {
				return true
			}
		}
		return false
	}

	if len(pathSegs) == 0 {
		return false
	}
	matched, err := path.Match(head, pathSegs[0])
	if err != nil || !matched {
		return false
	}

	return matchSegments(patternSegs[1:], pathSegs[1:])
}
