package pipeline

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"

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

	for i := range pf.Steps {
		hydrateStepRunAlias(&pf.Steps[i])
		if pf.Steps[i].Group == nil {
			continue
		}
		for j := range pf.Steps[i].Group.Steps {
			hydrateStepRunAlias(&pf.Steps[i].Group.Steps[j])
		}
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
	pipelineCache := resolveCache(pf.Pipeline.Cache)

	nodes := make([]ExecutionNode, 0, len(pf.Steps))
	frontier := make([]string, 0, 1)
	nextNodeIndex := 0

	for _, sd := range pf.Steps {
		if sd.Group == nil {
			step := resolveStepDef(sd, mergedPipelineEnv, pipelineCache)
			nodeID := buildNodeID(nextNodeIndex)
			nextNodeIndex++
			step.NodeID = nodeID
			step.GroupName = ""
			step.DependsOnNodeIDs = append([]string(nil), frontier...)
			nodes = append(nodes, ExecutionNode{
				NodeID:           nodeID,
				GroupName:        "",
				DependsOnNodeIDs: append([]string(nil), frontier...),
				Step:             step,
			})
			frontier = []string{nodeID}
			continue
		}

		groupName := strings.TrimSpace(sd.Group.Name)
		groupNodeIDs := make([]string, 0, len(sd.Group.Steps))
		groupDeps := append([]string(nil), frontier...)
		for _, groupStepDef := range sd.Group.Steps {
			step := resolveStepDef(groupStepDef, mergedPipelineEnv, pipelineCache)
			nodeID := buildNodeID(nextNodeIndex)
			nextNodeIndex++
			step.NodeID = nodeID
			step.GroupName = groupName
			step.DependsOnNodeIDs = append([]string(nil), groupDeps...)
			nodes = append(nodes, ExecutionNode{
				NodeID:           nodeID,
				GroupName:        groupName,
				DependsOnNodeIDs: append([]string(nil), groupDeps...),
				Step:             step,
			})
			groupNodeIDs = append(groupNodeIDs, nodeID)
		}
		frontier = groupNodeIDs
	}

	steps := make([]ResolvedStep, 0, len(nodes))
	for _, node := range nodes {
		steps = append(steps, node.Step)
	}

	return &ResolvedPipeline{
		Name:      pf.Pipeline.Name,
		Image:     strings.TrimSpace(pf.Pipeline.Image),
		Env:       mergedPipelineEnv,
		Steps:     steps,
		Plan:      ExecutionPlan{Nodes: nodes},
		Artifacts: ResolvedArtifacts{Paths: append([]string{}, pf.Artifacts.Paths...)},
		Cache:     pipelineCache.Clone(),
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

func hydrateStepRunAlias(step *StepDef) {
	if step == nil {
		return
	}
	if strings.TrimSpace(step.Run) == "" {
		step.Run = strings.TrimSpace(step.Command)
	}
}

func resolveStepDef(sd StepDef, pipelineEnv map[string]string, pipelineCache *domain.StepCacheConfig) ResolvedStep {
	merged := mergeEnv(pipelineEnv, sd.Env)

	timeout := 0
	if sd.TimeoutSeconds != nil {
		timeout = *sd.TimeoutSeconds
	}

	stepCache := pipelineCache
	if sd.Cache != nil {
		stepCache = resolveCache(sd.Cache)
	}

	return ResolvedStep{
		Name:           sd.Name,
		Image:          strings.TrimSpace(sd.Image),
		Run:            sd.Run,
		WorkingDir:     sd.WorkingDir,
		Env:            merged,
		TimeoutSeconds: timeout,
		ArtifactPaths:  append([]string{}, sd.Artifacts.Paths...),
		Cache:          stepCache.Clone(),
	}
}

func buildNodeID(index int) string {
	return fmt.Sprintf("node-%03d", index)
}
