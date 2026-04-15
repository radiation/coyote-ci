package domain

import "testing"

func TestFallbackNodeID(t *testing.T) {
	tests := []struct {
		name      string
		stepIndex int
		expected  string
	}{
		{name: "zero", stepIndex: 0, expected: "node-000"},
		{name: "single digit", stepIndex: 7, expected: "node-007"},
		{name: "triple digit", stepIndex: 123, expected: "node-123"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if actual := FallbackNodeID(tc.stepIndex); actual != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, actual)
			}
		})
	}
}
