package domain

import "strings"

type JobArtifactTrigger struct {
	ProducerJobID string
	Path          string
}

func NormalizeJobArtifactTrigger(trigger JobArtifactTrigger) JobArtifactTrigger {
	trigger.ProducerJobID = strings.TrimSpace(trigger.ProducerJobID)
	trigger.Path = strings.TrimSpace(trigger.Path)
	return trigger
}

func NormalizeJobArtifactTriggers(triggers []JobArtifactTrigger) []JobArtifactTrigger {
	if len(triggers) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(triggers))
	normalized := make([]JobArtifactTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		item := NormalizeJobArtifactTrigger(trigger)
		if item.ProducerJobID == "" || item.Path == "" {
			continue
		}
		key := item.ProducerJobID + "\x00" + item.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, item)
	}

	if len(normalized) == 0 {
		return nil
	}

	return normalized
}
