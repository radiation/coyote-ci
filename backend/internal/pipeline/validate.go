package pipeline

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
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

	// top-level env keys
	for key := range pf.Env {
		if !validEnvKey.MatchString(key) {
			errs = append(errs, ValidationError{Field: "env", Message: fmt.Sprintf("invalid env key %q", key)})
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

		if step.WorkingDir != "" && filepath.IsAbs(step.WorkingDir) {
			errs = append(errs, ValidationError{Field: prefix + ".working_dir", Message: "must be a relative path"})
		}

		for key := range step.Env {
			if !validEnvKey.MatchString(key) {
				errs = append(errs, ValidationError{Field: prefix + ".env", Message: fmt.Sprintf("invalid env key %q", key)})
			}
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
