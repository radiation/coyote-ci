package pipeline

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// PipelineFile is the top-level YAML-facing schema for a .coyote/pipeline.yml file.
// This type is used only for parsing; the rest of the system works with ResolvedPipeline.
type PipelineFile struct {
	Version   int               `yaml:"version"`
	Pipeline  PipelineMeta      `yaml:"pipeline"`
	Env       map[string]string `yaml:"env"`
	Steps     []StepDef         `yaml:"steps"`
	Artifacts ArtifactDef       `yaml:"artifacts"`
}

// PipelineMeta holds optional pipeline-level metadata from the YAML.
type PipelineMeta struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

// StepDef is the YAML-facing definition for a single step.
type StepDef struct {
	Name           string            `yaml:"name"`
	Image          string            `yaml:"image,omitempty"`
	Run            string            `yaml:"run"`
	Command        string            `yaml:"command,omitempty"`
	TimeoutSeconds *int              `yaml:"timeout_seconds"`
	WorkingDir     string            `yaml:"working_dir"`
	Env            map[string]string `yaml:"env"`
	Artifacts      ArtifactDef       `yaml:"artifacts,omitempty"`
}

// ArtifactDef holds optional build-level artifact path declarations.
type ArtifactDef struct {
	Paths []string `yaml:"paths"`
}

type artifactPathObject struct {
	Path string `yaml:"path"`
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
		return nil
	}

	switch node.Kind {
	case yaml.SequenceNode:
		paths := make([]string, 0, len(node.Content))
		for idx, item := range node.Content {
			switch item.Kind {
			case yaml.ScalarNode:
				paths = append(paths, item.Value)
			case yaml.MappingNode:
				var obj artifactPathObject
				if err := item.Decode(&obj); err != nil {
					return fmt.Errorf("invalid artifacts[%d] object: %w", idx, err)
				}
				paths = append(paths, obj.Path)
			default:
				return fmt.Errorf("artifacts[%d] must be a string or object with path", idx)
			}
		}
		d.Paths = paths
		return nil
	case yaml.MappingNode:
		var wrapper struct {
			Paths []string `yaml:"paths"`
		}
		if err := node.Decode(&wrapper); err != nil {
			return err
		}
		d.Paths = wrapper.Paths
		return nil
	case yaml.ScalarNode:
		// Allow a single scalar for convenience.
		d.Paths = []string{node.Value}
		return nil
	default:
		return fmt.Errorf("artifacts must be a sequence or mapping")
	}
}
