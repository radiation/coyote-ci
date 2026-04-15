package pipeline

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
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
	executableStepCount := 0
	for i, step := range pf.Steps {
		prefix := fmt.Sprintf("steps[%d]", i)
		if step.Group == nil {
			errs = append(errs, validateStepDef(step, prefix, seen)...)
			executableStepCount++
			continue
		}

		group := step.Group
		groupName := strings.TrimSpace(group.Name)
		if groupName == "" {
			errs = append(errs, ValidationError{Field: prefix + ".group.name", Message: "group name is required"})
		}
		if len(group.Steps) == 0 {
			errs = append(errs, ValidationError{Field: prefix + ".group.steps", Message: "group must contain at least one step"})
			continue
		}

		for j, groupStep := range group.Steps {
			if groupStep.Group != nil {
				errs = append(errs, ValidationError{Field: fmt.Sprintf("%s.group.steps[%d].group", prefix, j), Message: "nested groups are not allowed"})
				continue
			}
			errs = append(errs, validateStepDef(groupStep, fmt.Sprintf("%s.group.steps[%d]", prefix, j), seen)...)
			executableStepCount++
		}
	}

	if executableStepCount == 0 {
		errs = append(errs, ValidationError{Field: "steps", Message: "at least one step is required"})
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
	preset := strings.TrimSpace(def.Preset)
	if preset == "" {
		errs = append(errs, ValidationError{Field: fieldPrefix + ".preset", Message: "preset is required when cache is set"})
	} else if !cachepkg.IsSupportedPreset(preset) {
		errs = append(errs, ValidationError{Field: fieldPrefix + ".preset", Message: fmt.Sprintf("unknown cache preset %q", preset)})
	}

	policy := strings.TrimSpace(def.Policy)
	if policy != "" && !cachepkg.IsSupportedPolicy(policy) {
		errs = append(errs, ValidationError{Field: fieldPrefix + ".policy", Message: "policy must be one of: pull-push, pull, push, off"})
	}

	return errs
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

func validateStepDef(step StepDef, prefix string, seen map[string]bool) ValidationErrors {
	var errs ValidationErrors

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

	errCache := validateCacheDef(prefix+".cache", step.Cache)
	errs = append(errs, errCache...)

	return errs
}
