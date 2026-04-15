package domain

import "fmt"

// FallbackNodeID returns the canonical synthesized node identifier format used
// when graph metadata is absent but a stable per-step node id is needed.
func FallbackNodeID(stepIndex int) string {
	return fmt.Sprintf("node-%03d", stepIndex)
}
