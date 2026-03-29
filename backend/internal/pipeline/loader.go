package pipeline

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse decodes raw YAML bytes into a PipelineFile.
// Unknown YAML fields are rejected.
func Parse(data []byte) (*PipelineFile, error) {
	var pf PipelineFile
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&pf); err != nil {
		return nil, &ParseError{Err: err}
	}
	return &pf, nil
}

// ParseAndValidate parses YAML bytes and runs validation.
// Returns the parsed file on success, or a parse/validation error.
func ParseAndValidate(data []byte) (*PipelineFile, error) {
	pf, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(pf); err != nil {
		return nil, err
	}
	return pf, nil
}

// Resolve converts a validated PipelineFile into a ResolvedPipeline.
// Top-level env is merged into each step; step-level env wins on conflict.
func Resolve(pf *PipelineFile) *ResolvedPipeline {
	mergedPipelineEnv := copyEnv(pf.Env)

	steps := make([]ResolvedStep, 0, len(pf.Steps))
	for _, sd := range pf.Steps {
		merged := mergeEnv(mergedPipelineEnv, sd.Env)

		timeout := 0
		if sd.TimeoutSeconds != nil {
			timeout = *sd.TimeoutSeconds
		}

		steps = append(steps, ResolvedStep{
			Name:           sd.Name,
			Run:            sd.Run,
			WorkingDir:     sd.WorkingDir,
			Env:            merged,
			TimeoutSeconds: timeout,
		})
	}

	return &ResolvedPipeline{
		Name:  pf.Pipeline.Name,
		Env:   mergedPipelineEnv,
		Steps: steps,
	}
}

// LoadAndResolve is a convenience that parses, validates, and resolves YAML bytes.
func LoadAndResolve(data []byte) (*ResolvedPipeline, error) {
	pf, err := ParseAndValidate(data)
	if err != nil {
		return nil, err
	}
	return Resolve(pf), nil
}

// ConfigSource abstracts where pipeline config comes from.
type ConfigSource interface {
	// Load returns the raw YAML content and the source path/identifier.
	Load() (data []byte, sourcePath string, err error)
}

// FileSource loads pipeline config from a file path.
type FileSource struct {
	Path string
}

func (fs FileSource) Load() ([]byte, string, error) {
	data, err := os.ReadFile(fs.Path)
	if err != nil {
		return nil, fs.Path, fmt.Errorf("reading pipeline config %s: %w", fs.Path, err)
	}
	return data, fs.Path, nil
}

// ReaderSource loads pipeline config from an io.Reader (useful for testing).
type ReaderSource struct {
	Reader     io.Reader
	SourcePath string
}

func (rs ReaderSource) Load() ([]byte, string, error) {
	data, err := io.ReadAll(rs.Reader)
	if err != nil {
		return nil, rs.SourcePath, fmt.Errorf("reading pipeline config: %w", err)
	}
	return data, rs.SourcePath, nil
}

func copyEnv(src map[string]string) map[string]string {
	if src == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

func mergeEnv(base, override map[string]string) map[string]string {
	merged := copyEnv(base)
	for k, v := range override {
		merged[k] = v
	}
	return merged
}
