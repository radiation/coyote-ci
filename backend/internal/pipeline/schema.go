package pipeline

// PipelineFile is the top-level YAML-facing schema for a .coyote/pipeline.yml file.
// This type is used only for parsing; the rest of the system works with ResolvedPipeline.
type PipelineFile struct {
	Version  int               `yaml:"version"`
	Pipeline PipelineMeta      `yaml:"pipeline"`
	Env      map[string]string `yaml:"env"`
	Steps    []StepDef         `yaml:"steps"`
}

// PipelineMeta holds optional pipeline-level metadata from the YAML.
type PipelineMeta struct {
	Name string `yaml:"name"`
}

// StepDef is the YAML-facing definition for a single step.
type StepDef struct {
	Name           string            `yaml:"name"`
	Run            string            `yaml:"run"`
	TimeoutSeconds *int              `yaml:"timeout_seconds"`
	WorkingDir     string            `yaml:"working_dir"`
	Env            map[string]string `yaml:"env"`
}
