package pipeline

// ResolvedPipeline is the internal normalized representation of a pipeline config.
// YAML schema types do not leak beyond the pipeline package; the rest of the system
// works exclusively with this type.
type ResolvedPipeline struct {
	Name  string
	Env   map[string]string
	Steps []ResolvedStep
}

// ResolvedStep is a single normalized step ready for conversion to a canonical build step.
type ResolvedStep struct {
	Name           string
	Run            string
	WorkingDir     string
	Env            map[string]string
	TimeoutSeconds int
}
