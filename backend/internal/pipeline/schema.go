package pipeline

import (
	"fmt"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"gopkg.in/yaml.v3"
)

// PipelineFile is the top-level YAML-facing schema for a .coyote/pipeline.yml file.
// This type is used only for parsing; the rest of the system works with ResolvedPipeline.
type PipelineFile struct {
	Version   int               `yaml:"version"`
	Pipeline  PipelineMeta      `yaml:"pipeline"`
	Release   ReleaseMeta       `yaml:"release,omitempty"`
	Env       map[string]string `yaml:"env"`
	Steps     []StepDef         `yaml:"steps"`
	Artifacts ArtifactDef       `yaml:"artifacts"`
}

// PipelineMeta holds optional pipeline-level metadata from the YAML.
type PipelineMeta struct {
	Name  string    `yaml:"name"`
	Image string    `yaml:"image"`
	Cache *CacheDef `yaml:"cache,omitempty"`
}

type ReleaseMeta struct {
	Strategy string `yaml:"strategy,omitempty"`
	Version  string `yaml:"version,omitempty"`
	Template string `yaml:"template,omitempty"`
}

// StepDef is the YAML-facing definition for a single step.
type StepDef struct {
	Group          *StepGroupDef     `yaml:"group,omitempty"`
	Name           string            `yaml:"name"`
	Image          string            `yaml:"image,omitempty"`
	Run            string            `yaml:"run"`
	Command        string            `yaml:"command,omitempty"`
	TimeoutSeconds *int              `yaml:"timeout_seconds"`
	WorkingDir     string            `yaml:"working_dir"`
	Env            map[string]string `yaml:"env"`
	Artifacts      ArtifactDef       `yaml:"artifacts,omitempty"`
	Cache          *CacheDef         `yaml:"cache,omitempty"`
}

type StepGroupDef struct {
	Name  string    `yaml:"name"`
	Steps []StepDef `yaml:"steps"`
}

type CacheDef struct {
	Preset string `yaml:"preset,omitempty"`
	Policy string `yaml:"policy,omitempty"`
}

// ArtifactDef holds optional build-level artifact path declarations.
type ArtifactDef struct {
	Paths        []string                     `yaml:"paths"`
	Declarations []domain.ArtifactDeclaration `yaml:"-"`
}

type artifactPathObject struct {
	Name string `yaml:"name,omitempty"`
	Path string `yaml:"path"`
	Type string `yaml:"type,omitempty"`
}

// UnmarshalYAML supports ergonomic artifact declarations while normalizing
// into a single internal Paths representation.
//
// Supported forms:
//
//	artifacts:
//	  - dist/**
//	  - reports/*.xml
//
//	artifacts:
//	  - path: dist/**
//	  - path: reports/*.xml
//
//	artifacts:
//	  paths:
//	    - dist/**
//	    - reports/*.xml
func (d *ArtifactDef) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind == 0 {
		d.Paths = nil
		d.Declarations = nil
		return nil
	}

	declarations, err := parseArtifactDeclarationsNode(node)
	if err != nil {
		return err
	}
	d.Declarations = declarations
	d.Paths = make([]string, 0, len(declarations))
	for _, declaration := range declarations {
		d.Paths = append(d.Paths, declaration.Path)
	}
	return nil
}

func parseArtifactDeclarationsNode(node *yaml.Node) ([]domain.ArtifactDeclaration, error) {
	switch node.Kind {
	case yaml.SequenceNode:
		declarations := make([]domain.ArtifactDeclaration, 0, len(node.Content))
		for idx, item := range node.Content {
			declaration, err := parseArtifactDeclaration(item)
			if err != nil {
				return nil, fmt.Errorf("invalid artifacts[%d]: %w", idx, err)
			}
			declarations = append(declarations, declaration)
		}
		return declarations, nil
	case yaml.MappingNode:
		for idx := 0; idx+1 < len(node.Content); idx += 2 {
			if node.Content[idx].Value != "paths" {
				continue
			}
			return parseArtifactDeclarationsNode(node.Content[idx+1])
		}
		declaration, err := parseArtifactDeclaration(node)
		if err != nil {
			return nil, err
		}
		return []domain.ArtifactDeclaration{declaration}, nil
	case yaml.ScalarNode:
		return []domain.ArtifactDeclaration{{Path: node.Value}}, nil
	default:
		return nil, fmt.Errorf("artifacts must be a sequence or mapping")
	}
}

func parseArtifactDeclaration(node *yaml.Node) (domain.ArtifactDeclaration, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return domain.ArtifactDeclaration{Path: node.Value}, nil
	case yaml.MappingNode:
		var obj artifactPathObject
		if err := node.Decode(&obj); err != nil {
			return domain.ArtifactDeclaration{}, err
		}
		declaration := domain.ArtifactDeclaration{Name: obj.Name, Path: obj.Path}
		if artifactType, ok := domain.ParseArtifactType(obj.Type); ok {
			declaration.Type = artifactType
		}
		return declaration, nil
	default:
		return domain.ArtifactDeclaration{}, fmt.Errorf("must be a string or object with path")
	}
}
