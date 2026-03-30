package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
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
	BuildID          string
	WorkspacePath    string
	Patterns         []string
	SkipLogicalPaths map[string]struct{}
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

	storageRoot := ""
	if reporter, ok := c.store.(interface{ RootPath() string }); ok {
		storageRoot = strings.TrimSpace(reporter.RootPath())
	}
	log.Printf("artifact collection start: build_id=%s workspace_path=%s patterns=%q storage_root=%s", buildID, workspacePath, patterns, storageRoot)

	workspaceInfo, err := os.Stat(workspacePath)
	if err != nil {
		if os.IsNotExist(err) {
			log.Printf("artifact workspace check: build_id=%s workspace_path=%s exists=false readable=false", buildID, workspacePath)
			return CollectResult{Warnings: []string{fmt.Sprintf("workspace %q not found; skipping artifact collection", workspacePath)}}, nil
		}
		log.Printf("artifact workspace check failed: build_id=%s workspace_path=%s err=%v", buildID, workspacePath, err)
		return CollectResult{}, fmt.Errorf("checking workspace for artifact collection: %w", err)
	}
	if !workspaceInfo.IsDir() {
		log.Printf("artifact workspace check failed: build_id=%s workspace_path=%s exists=true readable=false err=workspace is not a directory", buildID, workspacePath)
		return CollectResult{}, fmt.Errorf("workspace %q is not a directory", workspacePath)
	}
	if _, err := os.ReadDir(workspacePath); err != nil {
		log.Printf("artifact workspace check failed: build_id=%s workspace_path=%s exists=true readable=false err=%v", buildID, workspacePath, err)
		return CollectResult{}, fmt.Errorf("reading workspace for artifact collection: %w", err)
	}
	log.Printf("artifact workspace check: build_id=%s workspace_path=%s exists=true readable=true", buildID, workspacePath)

	matchedByPattern := make(map[string]int, len(patterns))
	matchedPaths := map[string]struct{}{}
	selected := map[string]struct{}{}
	skippedCount := 0

	walkErr := filepath.WalkDir(workspacePath, func(absPath string, d fs.DirEntry, err error) error {
		if err != nil {
			log.Printf("artifact workspace scan error: build_id=%s workspace_path=%s err=%v", buildID, workspacePath, err)
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
			if !matchPathPattern(pattern, normalizedRel) {
				continue
			}

			matchedByPattern[pattern]++
			matchedPaths[normalizedRel] = struct{}{}
			if _, skipped := request.SkipLogicalPaths[normalizedRel]; skipped {
				skippedCount++
				log.Printf("artifact skip existing: build_id=%s logical_path=%s pattern=%s", buildID, normalizedRel, pattern)
				break
			}

			selected[normalizedRel] = struct{}{}
			break
		}

		return nil
	})
	if walkErr != nil {
		log.Printf("artifact workspace scan failed: build_id=%s workspace_path=%s err=%v", buildID, workspacePath, walkErr)
		return CollectResult{}, fmt.Errorf("walking workspace for artifact collection: %w", walkErr)
	}

	warnings := make([]string, 0)
	for _, pattern := range patterns {
		count := matchedByPattern[pattern]
		if count == 0 {
			log.Printf("artifact pattern evaluation: build_id=%s pattern=%s resolution=workspace-relative-glob matches=0", buildID, pattern)
			log.Printf("artifact pattern unmatched: build_id=%s pattern=%s", buildID, pattern)
			warnings = append(warnings, fmt.Sprintf("artifact pattern %q matched no files", pattern))
			continue
		}
		log.Printf("artifact pattern evaluation: build_id=%s pattern=%s resolution=workspace-relative-glob matches=%d", buildID, pattern, count)
	}

	artifacts := make([]CollectedArtifact, 0, len(selected))
	failedCollectErrors := make([]error, 0)
	selectedPaths := make([]string, 0, len(selected))
	for relPath := range selected {
		selectedPaths = append(selectedPaths, relPath)
	}
	sort.Strings(selectedPaths)

	for _, relPath := range selectedPaths {
		artifact, err := c.collectSingle(ctx, buildID, workspacePath, relPath)
		if err != nil {
			log.Printf("artifact persistence error: build_id=%s logical_path=%s err=%v", buildID, relPath, err)
			failedCollectErrors = append(failedCollectErrors, fmt.Errorf("failed collecting artifact %q: %w", relPath, err))
			continue
		}
		artifacts = append(artifacts, artifact)
	}

	result := CollectResult{Artifacts: artifacts, Warnings: warnings}
	log.Printf("artifact collection summary: build_id=%s matched=%d persisted=%d skipped=%d warnings=%d errors=%d", buildID, len(matchedPaths), len(artifacts), skippedCount, len(warnings), len(failedCollectErrors))
	if len(failedCollectErrors) > 0 {
		return result, errors.Join(failedCollectErrors...)
	}

	return result, nil
}

func (c *Collector) collectSingle(ctx context.Context, buildID string, workspacePath string, logicalPath string) (CollectedArtifact, error) {
	absPath := filepath.Join(workspacePath, filepath.FromSlash(logicalPath))
	resolvedWorkspacePath, err := filepath.EvalSymlinks(workspacePath)
	if err != nil {
		resolvedWorkspacePath = workspacePath
	}

	fileInfo, err := os.Lstat(absPath)
	if err != nil {
		return CollectedArtifact{}, fmt.Errorf("stating source artifact: %w", err)
	}
	if fileInfo.Mode()&fs.ModeSymlink != 0 {
		return CollectedArtifact{}, fmt.Errorf("artifact %q is a symlink and cannot be collected", logicalPath)
	}

	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return CollectedArtifact{}, fmt.Errorf("resolving source artifact path: %w", err)
	}
	if !pathWithinBase(resolvedWorkspacePath, resolvedPath) {
		return CollectedArtifact{}, fmt.Errorf("artifact %q resolves outside workspace", logicalPath)
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return CollectedArtifact{}, fmt.Errorf("opening source artifact: %w", err)
	}
	defer func() {
		_ = file.Close()
	}()

	openedInfo, err := file.Stat()
	if err != nil {
		return CollectedArtifact{}, fmt.Errorf("reading source artifact metadata: %w", err)
	}
	if !openedInfo.Mode().IsRegular() {
		return CollectedArtifact{}, fmt.Errorf("artifact %q must be a regular file", logicalPath)
	}

	hasher := sha256.New()
	tee := io.TeeReader(file, hasher)
	storageKey := path.Join(buildID, logicalPath)
	storagePath := storageKey
	if reporter, ok := c.store.(interface{ RootPath() string }); ok {
		root := strings.TrimSpace(reporter.RootPath())
		if root != "" {
			storagePath = filepath.Join(root, filepath.FromSlash(storageKey))
		}
	}
	log.Printf("artifact persist start: build_id=%s logical_path=%s source_path=%s resolved_source_path=%s storage_key=%s storage_path=%s size_bytes=%d", buildID, logicalPath, absPath, resolvedPath, storageKey, storagePath, openedInfo.Size())
	size, err := c.store.Save(ctx, storageKey, tee)
	if err != nil {
		return CollectedArtifact{}, fmt.Errorf("saving artifact to store: %w", err)
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

func pathWithinBase(basePath string, candidatePath string) bool {
	cleanBase := filepath.Clean(basePath)
	cleanCandidate := filepath.Clean(candidatePath)

	rel, err := filepath.Rel(cleanBase, cleanCandidate)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	return true
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
