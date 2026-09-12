package pipeline

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/versioning"
)

// validEnvKey matches POSIX-style environment variable names: letters, digits, underscore, starting with letter or underscore.
var validEnvKey = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// Validate checks a parsed PipelineFile for semantic correctness.
// Returns nil on success or a ValidationErrors with all problems found.
func Validate(pf *PipelineFile) error {
	var errs ValidationErrors
	var dependencyRefs []stepDependencyRef

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

	for i, declaration := range declarationsForValidation(pf.Artifacts) {
		trimmed := strings.TrimSpace(declaration.Path)
		field := fmt.Sprintf("artifacts.paths[%d]", i)
		if trimmed == "" {
			errs = append(errs, ValidationError{Field: field, Message: "artifact path is required"})
			continue
		}

		if err := validateArtifactPathPattern(trimmed); err != nil {
			errs = append(errs, ValidationError{Field: field, Message: err.Error()})
		}
		if declaration.Type != "" {
			if _, ok := domain.ParseArtifactType(string(declaration.Type)); !ok {
				errs = append(errs, ValidationError{Field: field + ".type", Message: fmt.Sprintf("unsupported artifact type %q", declaration.Type)})
			}
		}
		if declaration.Version != nil {
			if err := versioning.ValidateArtifactVersionConfig(declaration.Version.Template, declaration.Version.Channel); err != nil {
				errs = append(errs, ValidationError{Field: field + ".version", Message: err.Error()})
			}
		}
		if strings.TrimSpace(declaration.Name) != "" && pathPatternHasWildcard(trimmed) {
			errs = append(errs, ValidationError{Field: field + ".name", Message: "artifact name requires an exact path declaration"})
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
	frontier := make([]string, 0, 1)
	for i, step := range pf.Steps {
		prefix := fmt.Sprintf("steps[%d]", i)
		if step.Group == nil {
			errs = append(errs, validateStepDef(step, prefix, seen)...)
			dependencyRefs = append(dependencyRefs, stepDependencyRef{name: step.Name, dependsOn: step.DependsOn, implicitDependencies: append([]string(nil), frontier...), field: prefix + ".depends_on"})
			frontier = []string{step.Name}
			executableStepCount++
			continue
		}

		errs = append(errs, validateGroupWrapperStep(step, prefix)...)

		groupName := strings.TrimSpace(step.Group.Name)
		if groupName == "" {
			errs = append(errs, ValidationError{Field: prefix + ".group.name", Message: "group name is required"})
		}
		if len(step.Group.Steps) == 0 {
			errs = append(errs, ValidationError{Field: prefix + ".group.steps", Message: "group must contain at least one step"})
			continue
		}

		groupDependencies := append([]string(nil), frontier...)
		groupFrontier := make([]string, 0, len(step.Group.Steps))
		for j, groupStep := range step.Group.Steps {
			if groupStep.Group != nil {
				errs = append(errs, ValidationError{Field: fmt.Sprintf("%s.group.steps[%d].group", prefix, j), Message: "nested groups are not allowed"})
				continue
			}
			errs = append(errs, validateStepDef(groupStep, fmt.Sprintf("%s.group.steps[%d]", prefix, j), seen)...)
			dependencyRefs = append(dependencyRefs, stepDependencyRef{name: groupStep.Name, dependsOn: groupStep.DependsOn, implicitDependencies: groupDependencies, field: fmt.Sprintf("%s.group.steps[%d].depends_on", prefix, j)})
			groupFrontier = append(groupFrontier, groupStep.Name)
			executableStepCount++
		}
		frontier = groupFrontier
	}

	if executableStepCount == 0 {
		errs = append(errs, ValidationError{Field: "steps", Message: "at least one step is required"})
	}
	errs = append(errs, validateStepDependencies(dependencyRefs)...)

	if len(errs) > 0 {
		return errs
	}
	return nil
}

type stepDependencyRef struct {
	name                 string
	dependsOn            *[]string
	implicitDependencies []string
	field                string
}

func validateStepDependencies(refs []stepDependencyRef) ValidationErrors {
	var errs ValidationErrors
	nodeIDsByName := make(map[string]string, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref.name)
		if name != "" {
			nodeIDsByName[strings.ToLower(name)] = name
		}
	}

	dependenciesByName := make(map[string][]string, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref.name)
		if name == "" {
			continue
		}
		dependencies := ref.implicitDependencies
		if ref.dependsOn != nil {
			dependencies = *ref.dependsOn
		}
		seenDependencies := make(map[string]struct{}, len(dependencies))
		for index, dependency := range dependencies {
			trimmed := strings.TrimSpace(dependency)
			key := strings.ToLower(trimmed)
			field := fmt.Sprintf("%s[%d]", ref.field, index)
			if trimmed == "" || nodeIDsByName[key] == "" {
				errs = append(errs, ValidationError{Field: field, Message: fmt.Sprintf("unknown dependency %q", dependency)})
				continue
			}
			if key == strings.ToLower(name) {
				errs = append(errs, ValidationError{Field: field, Message: "step cannot depend on itself"})
				continue
			}
			if _, duplicate := seenDependencies[key]; duplicate {
				errs = append(errs, ValidationError{Field: field, Message: fmt.Sprintf("duplicate dependency %q", dependency)})
				continue
			}
			seenDependencies[key] = struct{}{}
			dependenciesByName[strings.ToLower(name)] = append(dependenciesByName[strings.ToLower(name)], key)
		}
	}
	if hasDependencyCycle(dependenciesByName) {
		errs = append(errs, ValidationError{Field: "steps", Message: "step dependencies must not contain a cycle"})
	}
	return errs
}

func hasDependencyCycle(dependenciesByName map[string][]string) bool {
	states := make(map[string]uint8, len(dependenciesByName))
	var visit func(string) bool
	visit = func(node string) bool {
		switch states[node] {
		case 1:
			return true
		case 2:
			return false
		}
		states[node] = 1
		for _, dependency := range dependenciesByName[node] {
			if visit(dependency) {
				return true
			}
		}
		states[node] = 2
		return false
	}
	for node := range dependenciesByName {
		if visit(node) {
			return true
		}
	}
	return false
}

func validateGroupWrapperStep(step StepDef, prefix string) ValidationErrors {
	var errs ValidationErrors

	if strings.TrimSpace(step.Name) != "" {
		errs = append(errs, ValidationError{Field: prefix + ".name", Message: "group wrapper must not set name"})
	}
	if step.DependsOn != nil {
		errs = append(errs, ValidationError{Field: prefix + ".depends_on", Message: "group wrapper must not set depends_on"})
	}
	if strings.TrimSpace(step.Image) != "" {
		errs = append(errs, ValidationError{Field: prefix + ".image", Message: "group wrapper must not set image"})
	}
	if strings.TrimSpace(step.Run) != "" {
		errs = append(errs, ValidationError{Field: prefix + ".run", Message: "group wrapper must not set run"})
	}
	if strings.TrimSpace(step.Command) != "" {
		errs = append(errs, ValidationError{Field: prefix + ".command", Message: "group wrapper must not set command"})
	}
	if step.TimeoutSeconds != nil {
		errs = append(errs, ValidationError{Field: prefix + ".timeout_seconds", Message: "group wrapper must not set timeout_seconds"})
	}
	if strings.TrimSpace(step.WorkingDir) != "" {
		errs = append(errs, ValidationError{Field: prefix + ".working_dir", Message: "group wrapper must not set working_dir"})
	}
	if len(step.Env) > 0 {
		errs = append(errs, ValidationError{Field: prefix + ".env", Message: "group wrapper must not set env"})
	}
	if len(step.Artifacts.Paths) > 0 {
		errs = append(errs, ValidationError{Field: prefix + ".artifacts", Message: "group wrapper must not set artifacts"})
	}
	if step.Cache != nil {
		errs = append(errs, ValidationError{Field: prefix + ".cache", Message: "group wrapper must not set cache"})
	}

	return errs
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
		if step.ImageBuild == nil {
			errs = append(errs, ValidationError{Field: prefix + ".run", Message: "run command is required"})
		}
	} else if step.ImageBuild != nil {
		errs = append(errs, ValidationError{Field: prefix, Message: "step must specify exactly one execution kind"})
	}
	if step.ImageBuild != nil {
		if strings.TrimSpace(step.Image) != "" {
			errs = append(errs, ValidationError{Field: prefix + ".image", Message: "image_build step must not set container image"})
		}
		if strings.TrimSpace(step.ImageBuild.Context) == "" {
			errs = append(errs, ValidationError{Field: prefix + ".image_build.context", Message: "is required"})
		}
		if strings.TrimSpace(step.ImageBuild.Dockerfile) == "" {
			errs = append(errs, ValidationError{Field: prefix + ".image_build.dockerfile", Message: "is required"})
		}
		if strings.TrimSpace(step.ImageBuild.Image) == "" {
			errs = append(errs, ValidationError{Field: prefix + ".image_build.image", Message: "is required"})
		}
		for key, value := range step.ImageBuild.BuildArgs {
			if !validEnvKey.MatchString(key) || strings.TrimSpace(value) == "" {
				errs = append(errs, ValidationError{Field: prefix + ".image_build.build_args", Message: fmt.Sprintf("invalid build argument %q", key)})
			}
		}
	}

	if step.TimeoutSeconds != nil && *step.TimeoutSeconds <= 0 {
		errs = append(errs, ValidationError{Field: prefix + ".timeout_seconds", Message: "must be > 0 when set"})
	}

	if step.WorkingDir != "" {
		cleaned := filepath.Clean(step.WorkingDir)
		if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			errs = append(errs, ValidationError{Field: prefix + ".working_dir", Message: "must be a relative path"})
		}
	}

	for key := range step.Env {
		if !validEnvKey.MatchString(key) {
			errs = append(errs, ValidationError{Field: prefix + ".env", Message: fmt.Sprintf("invalid env key %q", key)})
		}
	}

	for j, declaration := range declarationsForValidation(step.Artifacts) {
		trimmed := strings.TrimSpace(declaration.Path)
		field := fmt.Sprintf("%s.artifacts.paths[%d]", prefix, j)
		if trimmed == "" {
			errs = append(errs, ValidationError{Field: field, Message: "artifact path is required"})
			continue
		}
		if err := validateArtifactPathPattern(trimmed); err != nil {
			errs = append(errs, ValidationError{Field: field, Message: err.Error()})
		}
		if declaration.Type != "" {
			if _, ok := domain.ParseArtifactType(string(declaration.Type)); !ok {
				errs = append(errs, ValidationError{Field: field + ".type", Message: fmt.Sprintf("unsupported artifact type %q", declaration.Type)})
			}
		}
		if declaration.Version != nil {
			if err := versioning.ValidateArtifactVersionConfig(declaration.Version.Template, declaration.Version.Channel); err != nil {
				errs = append(errs, ValidationError{Field: field + ".version", Message: err.Error()})
			}
		}
	}

	errs = append(errs, validateCacheDef(prefix+".cache", step.Cache)...)
	return errs
}

func declarationsForValidation(def ArtifactDef) []domain.ArtifactDeclaration {
	if len(def.Declarations) > 0 {
		return def.Declarations
	}
	declarations := make([]domain.ArtifactDeclaration, 0, len(def.Paths))
	for _, path := range def.Paths {
		declarations = append(declarations, domain.ArtifactDeclaration{Path: path})
	}
	return declarations
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

func pathPatternHasWildcard(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}
