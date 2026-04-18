package domain

import "fmt"

func FallbackNodeID(stepIndex int) string {
	return fmt.Sprintf("node-%03d", stepIndex)
}
