package pipeline

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func PipelineImageRef(data []byte) (string, error) {
	root := yaml.Node{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", err
	}

	doc, err := pipelineDocMapping(&root)
	if err != nil {
		return "", err
	}

	pipelineNode := mappingValue(doc, "pipeline")
	if pipelineNode == nil || pipelineNode.Kind != yaml.MappingNode {
		return "", nil
	}

	imageNode := mappingValue(pipelineNode, "image")
	if imageNode == nil {
		return "", nil
	}

	return strings.TrimSpace(imageNode.Value), nil
}

// UpdatePipelineImageRef updates only pipeline.image and leaves unrelated fields untouched.
func UpdatePipelineImageRef(data []byte, imageRef string) ([]byte, bool, error) {
	trimmedImageRef := strings.TrimSpace(imageRef)
	if trimmedImageRef == "" {
		return nil, false, fmt.Errorf("image ref is required")
	}

	root := yaml.Node{}
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, false, err
	}

	doc, err := pipelineDocMapping(&root)
	if err != nil {
		return nil, false, err
	}

	pipelineNode := ensureMappingValue(doc, "pipeline")
	imageNode := mappingValue(pipelineNode, "image")
	if imageNode != nil && strings.TrimSpace(imageNode.Value) == trimmedImageRef {
		return data, false, nil
	}

	if imageNode == nil {
		setMappingValue(pipelineNode, "image", &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: trimmedImageRef})
	} else {
		imageNode.Kind = yaml.ScalarNode
		imageNode.Tag = "!!str"
		imageNode.Value = trimmedImageRef
	}

	out := bytes.Buffer{}
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		return nil, false, err
	}
	if err := enc.Close(); err != nil {
		return nil, false, err
	}

	return out.Bytes(), true, nil
}

func pipelineDocMapping(root *yaml.Node) (*yaml.Node, error) {
	if len(root.Content) == 0 {
		return nil, fmt.Errorf("pipeline document is empty")
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("pipeline document must be a mapping")
	}
	return doc, nil
}

func mappingValue(mappingNode *yaml.Node, key string) *yaml.Node {
	if mappingNode == nil || mappingNode.Kind != yaml.MappingNode {
		return nil
	}
	for idx := 0; idx+1 < len(mappingNode.Content); idx += 2 {
		k := mappingNode.Content[idx]
		if strings.TrimSpace(k.Value) == key {
			return mappingNode.Content[idx+1]
		}
	}
	return nil
}

func ensureMappingValue(mappingNode *yaml.Node, key string) *yaml.Node {
	current := mappingValue(mappingNode, key)
	if current != nil {
		if current.Kind != yaml.MappingNode {
			current.Kind = yaml.MappingNode
			current.Tag = "!!map"
			current.Content = nil
		}
		return current
	}

	newKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	newValue := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	mappingNode.Content = append(mappingNode.Content, newKey, newValue)
	return newValue
}

func setMappingValue(mappingNode *yaml.Node, key string, value *yaml.Node) {
	if mappingNode == nil || mappingNode.Kind != yaml.MappingNode {
		return
	}
	for idx := 0; idx+1 < len(mappingNode.Content); idx += 2 {
		k := mappingNode.Content[idx]
		if strings.TrimSpace(k.Value) == key {
			mappingNode.Content[idx+1] = value
			return
		}
	}
	newKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	mappingNode.Content = append(mappingNode.Content, newKey, value)
}
