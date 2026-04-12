package pipeline

import "github.com/radiation/coyote-ci/backend/internal/domain"

// ResolvedPipeline is the internal normalized representation of a pipeline config.
// YAML schema types do not leak beyond the pipeline package; the rest of the system
// works exclusively with this type.
type ResolvedPipeline struct {
	Name      string
	Image     string
	Env       map[string]string
	Steps     []ResolvedStep
	Artifacts ResolvedArtifacts
	Cache     *domain.StepCacheConfig
}

// ResolvedStep is a single normalized step ready for conversion to a canonical build step.
type ResolvedStep struct {
	Name           string
	Image          string
	Run            string
	WorkingDir     string
	Env            map[string]string
	TimeoutSeconds int
	ArtifactPaths  []string
	Cache          *domain.StepCacheConfig
}

// ResolvedArtifacts captures normalized build-level artifact paths.
type ResolvedArtifacts struct {
	Paths []string
}
