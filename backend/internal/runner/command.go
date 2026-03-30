package runner

import (
	"strconv"
	"strings"
)

// RenderStepCommand renders a stable, human-readable command representation
// from the same command/args inputs used for step execution.
func RenderStepCommand(command string, args []string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return ""
	}

	// Most pipeline steps are executed as sh -c <script>; render the script
	// body directly for easier readability in logs.
	if trimmed == "sh" && len(args) >= 2 && args[0] == "-c" {
		return args[1]
	}

	parts := make([]string, 0, len(args)+1)
	parts = append(parts, strconv.Quote(trimmed))
	for _, arg := range args {
		parts = append(parts, strconv.Quote(arg))
	}

	return strings.Join(parts, " ")
}
