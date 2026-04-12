package pipeline

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

// validEnvKey matches POSIX-style environment variable names: letters, digits, underscore, starting with letter or underscore.
var validEnvKey = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Validate checks a parsed PipelineFile for semantic correctness.
// Returns nil on success or a ValidationErrors with all problems found.
func Validate(pf *PipelineFile) error {
	var errs ValidationErrors

	// version
	if pf.Version != 1 {
		errs = append(errs, ValidationError{Field: "version", Message: fmt.Sprintf("unsupported version %d, must be 1", pf.Version)})
	}

	// optional pipeline image
	if pf.Pipeline.Image != "" {
		if strings.TrimSpace(pf.Pipeline.Image) == "" {
			errs = append(errs, ValidationError{Field: "pipeline.image", Message: "must be non-empty when set"})
		}
	}

	errCache := validateCacheDef("pipeline.cache", pf.Pipeline.Cache)
	errs = append(errs, errCache...)

	// top-level env keys
	for key := range pf.Env {
		if !validEnvKey.MatchString(key) {
			errs = append(errs, ValidationError{Field: "env", Message: fmt.Sprintf("invalid env key %q", key)})
		}
	}

	for i, pattern := range pf.Artifacts.Paths {
		trimmed := strings.TrimSpace(pattern)
		field := fmt.Sprintf("artifacts.paths[%d]", i)
		if trimmed == "" {
			errs = append(errs, ValidationError{Field: field, Message: "artifact path is required"})
			continue
		}

		if err := validateArtifactPathPattern(trimmed); err != nil {
			errs = append(errs, ValidationError{Field: field, Message: err.Error()})
		}
	}

	// steps presence
	if len(pf.Steps) == 0 {
		errs = append(errs, ValidationError{Field: "steps", Message: "at least one step is required"})
		return errs
	}

	// step-level validation
	seen := make(map[string]bool, len(pf.Steps))
	for i, step := range pf.Steps {
		prefix := fmt.Sprintf("steps[%d]", i)

		name := strings.TrimSpace(step.Name)
		if name == "" {
			errs = append(errs, ValidationError{Field: prefix + ".name", Message: "step name is required"})
		} else {
			lower := strings.ToLower(name)
			if seen[lower] {
				errs = append(errs, ValidationError{Field: prefix + ".name", Message: fmt.Sprintf("duplicate step name %q", name)})
			}
			seen[lower] = true
		}

		if strings.TrimSpace(step.Run) == "" {
			errs = append(errs, ValidationError{Field: prefix + ".run", Message: "run command is required"})
		}

		if step.TimeoutSeconds != nil && *step.TimeoutSeconds <= 0 {
			errs = append(errs, ValidationError{Field: prefix + ".timeout_seconds", Message: "must be > 0 when set"})
		}

		if step.WorkingDir != "" {
			cleaned := filepath.Clean(step.WorkingDir)
			if filepath.IsAbs(cleaned) ||
				cleaned == ".." ||
				strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
				errs = append(errs, ValidationError{Field: prefix + ".working_dir", Message: "must be a relative path"})
			}
		}

		for key := range step.Env {
			if !validEnvKey.MatchString(key) {
				errs = append(errs, ValidationError{Field: prefix + ".env", Message: fmt.Sprintf("invalid env key %q", key)})
			}
		}

		for j, pattern := range step.Artifacts.Paths {
			trimmed := strings.TrimSpace(pattern)
			field := fmt.Sprintf("%s.artifacts.paths[%d]", prefix, j)
			if trimmed == "" {
				errs = append(errs, ValidationError{Field: field, Message: "artifact path is required"})
				continue
			}
			if err := validateArtifactPathPattern(trimmed); err != nil {
				errs = append(errs, ValidationError{Field: field, Message: err.Error()})
			}
		}

		stepCacheErrs := validateCacheDef(prefix+".cache", step.Cache)
		errs = append(errs, stepCacheErrs...)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateCacheDef(fieldPrefix string, def *CacheDef) ValidationErrors {
	if def == nil {
		return nil
	}

	var errs ValidationErrors
	scope := strings.TrimSpace(def.Scope)
	if scope == "" {
		errs = append(errs, ValidationError{Field: fieldPrefix + ".scope", Message: "scope is required when cache is set"})
	} else if scope != string(domain.CacheScopeBuild) && scope != string(domain.CacheScopeJob) {
		errs = append(errs, ValidationError{Field: fieldPrefix + ".scope", Message: "scope must be one of: build, job"})
	}

	preset := strings.TrimSpace(def.Preset)
	if preset != "" {
		if _, _, ok := presetValues(preset); !ok {
			errs = append(errs, ValidationError{Field: fieldPrefix + ".preset", Message: fmt.Sprintf("unknown cache preset %q", preset)})
		}
	}

	for i, rawPath := range def.Paths {
		cachePath := strings.TrimSpace(rawPath)
		pathField := fmt.Sprintf("%s.paths[%d]", fieldPrefix, i)
		if cachePath == "" {
			errs = append(errs, ValidationError{Field: pathField, Message: "cache path is required"})
			continue
		}
		if strings.ContainsRune(cachePath, '\\') {
			errs = append(errs, ValidationError{Field: pathField, Message: "cache path must use forward slashes"})
			continue
		}
		if !path.IsAbs(cachePath) {
			errs = append(errs, ValidationError{Field: pathField, Message: "cache path must be an absolute container path"})
		}
	}

	for i, rawFile := range def.Key.Files {
		file := strings.TrimSpace(rawFile)
		fileField := fmt.Sprintf("%s.key.files[%d]", fieldPrefix, i)
		if file == "" {
			errs = append(errs, ValidationError{Field: fileField, Message: "cache key file is required"})
			continue
		}
		if strings.ContainsRune(file, '\\') {
			errs = append(errs, ValidationError{Field: fileField, Message: "cache key file must use forward slashes"})
			continue
		}
		if path.IsAbs(file) {
			errs = append(errs, ValidationError{Field: fileField, Message: "cache key file must be workspace-relative"})
			continue
		}
		cleaned := path.Clean(file)
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
			errs = append(errs, ValidationError{Field: fileField, Message: "cache key file must stay within workspace"})
		}
	}

	resolved := resolveCache(def)
	if resolved == nil || len(resolved.Paths) == 0 {
		errs = append(errs, ValidationError{Field: fieldPrefix + ".paths", Message: "resolved cache paths must be non-empty"})
	}
	if resolved == nil || len(resolved.KeyFiles) == 0 {
		errs = append(errs, ValidationError{Field: fieldPrefix + ".key.files", Message: "resolved cache key files must be non-empty"})
	}

	return errs
}

func presetValues(name string) ([]string, []string, bool) {
	paths, keyFiles := cachePresetDefaults(name)
	return paths, keyFiles, len(paths) > 0 || len(keyFiles) > 0
}

func validateArtifactPathPattern(pattern string) error {
	if strings.ContainsRune(pattern, '\\') {
		return fmt.Errorf("artifact path must use forward slashes")
	}
	if strings.HasPrefix(pattern, "/") {
		return fmt.Errorf("artifact path must be relative")
	}

	for _, seg := range strings.Split(pattern, "/") {
		if seg == ".." {
			return fmt.Errorf("artifact path must stay within workspace")
		}
	}

	return nil
}
