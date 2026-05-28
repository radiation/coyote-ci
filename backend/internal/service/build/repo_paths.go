package build

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/pipeline"
)

func resolveRepoPipelinePath(repoRoot string, requestedPath string) (string, string, error) {
	trimmed := strings.TrimSpace(requestedPath)
	if trimmed == "" {
		trimmed = pipelineFilePath
	}

	if filepath.IsAbs(trimmed) {
		return "", "", fmt.Errorf("%w: must be a relative path", ErrInvalidPipelinePath)
	}

	cleaned := filepath.Clean(trimmed)
	if cleaned == "." {
		return "", "", fmt.Errorf("%w: must point to a file", ErrInvalidPipelinePath)
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("%w: must stay within repository root", ErrInvalidPipelinePath)
	}
	if filepath.VolumeName(cleaned) != "" {
		return "", "", fmt.Errorf("%w: must not include a volume prefix", ErrInvalidPipelinePath)
	}

	abs := filepath.Join(repoRoot, cleaned)
	rel, err := filepath.Rel(repoRoot, abs)
	if err != nil {
		return "", "", fmt.Errorf("%w: unable to resolve path", ErrInvalidPipelinePath)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", "", fmt.Errorf("%w: must stay within repository root", ErrInvalidPipelinePath)
	}

	normalized := filepath.ToSlash(cleaned)
	return abs, normalized, nil
}

func resolveRepoStepWorkingDirs(pipelinePath string, resolved *pipeline.ResolvedPipeline) (*pipeline.ResolvedPipeline, error) {
	if resolved == nil {
		return nil, fmt.Errorf("%w: pipeline is required", ErrInvalidPipelinePath)
	}

	normalizedPipelinePath := path.Clean(filepath.ToSlash(strings.TrimSpace(pipelinePath)))
	pipelineDir := "."
	if normalizedPipelinePath != pipelineFilePath {
		pipelineDir = path.Clean(path.Dir(normalizedPipelinePath))
		if pipelineDir == "" {
			pipelineDir = "."
		}
	}

	for i := range resolved.Steps {
		stepDir := strings.TrimSpace(resolved.Steps[i].WorkingDir)
		if stepDir == "" || stepDir == "." {
			resolved.Steps[i].WorkingDir = pipelineDir
			continue
		}

		if path.IsAbs(stepDir) {
			return nil, fmt.Errorf("%w: steps[%d].working_dir must be relative", ErrInvalidPipelinePath, i)
		}

		normalizedStepDir := path.Clean(strings.ReplaceAll(stepDir, "\\", "/"))
		if normalizedStepDir == ".." || strings.HasPrefix(normalizedStepDir, "../") {
			return nil, fmt.Errorf("%w: steps[%d].working_dir escapes repository root", ErrInvalidPipelinePath, i)
		}

		combined := path.Clean(path.Join(pipelineDir, normalizedStepDir))
		if combined == ".." || strings.HasPrefix(combined, "../") {
			return nil, fmt.Errorf("%w: steps[%d].working_dir escapes repository root", ErrInvalidPipelinePath, i)
		}

		resolved.Steps[i].WorkingDir = combined
	}

	return resolved, nil
}
