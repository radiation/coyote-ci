package runner

import "testing"

func TestRenderStepCommand(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		args     []string
		expected string
	}{
		{name: "empty command", command: "", args: nil, expected: ""},
		{name: "shell script renders body", command: "sh", args: []string{"-c", "echo hello"}, expected: "echo hello"},
		{name: "generic command quoted", command: "go", args: []string{"test", "./..."}, expected: "\"go\" \"test\" \"./...\""},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := RenderStepCommand(tc.command, tc.args); got != tc.expected {
				t.Fatalf("expected %q, got %q", tc.expected, got)
			}
		})
	}
}
