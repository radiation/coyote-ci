package architecture_test

import (
	"strings"
	"testing"

	archgo "github.com/arch-go/arch-go/v2/api"
	"github.com/arch-go/arch-go/v2/api/configuration"
)

const backendModulePath = "github.com/radiation/coyote-ci/backend"

func TestBackendArchitectureGuardrails(t *testing.T) {
	t.Helper()

	moduleInfo := configuration.Load(backendModulePath)
	result := archgo.CheckArchitecture(moduleInfo, configuration.Config{
		DependenciesRules: []*configuration.DependenciesRule{
			{
				Package: backendModulePath + "/internal/domain",
				ShouldNotDependsOn: internalDeps(
					backendModulePath+"/internal/http",
					backendModulePath+"/internal/http/handler",
					backendModulePath+"/internal/service",
					backendModulePath+"/internal/repository/postgres",
					backendModulePath+"/internal/repository/memory",
					backendModulePath+"/cmd/server",
					backendModulePath+"/cmd/worker",
				),
			},
			{
				Package: backendModulePath + "/internal/http/handler",
				ShouldNotDependsOn: internalDeps(
					backendModulePath+"/internal/repository/postgres",
					backendModulePath+"/internal/repository/memory",
				),
			},
			{
				Package: backendModulePath + "/internal/service",
				ShouldNotDependsOn: internalDeps(
					backendModulePath+"/internal/http",
					backendModulePath+"/internal/http/handler",
					backendModulePath+"/cmd/server",
					backendModulePath+"/cmd/worker",
					backendModulePath+"/internal/repository/postgres",
					backendModulePath+"/internal/repository/memory",
				),
			},
			{
				Package: "**.internal/service.**",
				ShouldNotDependsOn: internalDeps(
					backendModulePath+"/internal/http",
					backendModulePath+"/internal/http/handler",
					backendModulePath+"/cmd/server",
					backendModulePath+"/cmd/worker",
					backendModulePath+"/internal/repository/postgres",
					backendModulePath+"/internal/repository/memory",
				),
			},
			{
				Package: backendModulePath + "/internal/repository/postgres",
				ShouldNotDependsOn: internalDeps(
					backendModulePath+"/internal/http",
					backendModulePath+"/internal/http/handler",
					backendModulePath+"/internal/service",
					backendModulePath+"/cmd/server",
					backendModulePath+"/cmd/worker",
				),
			},
			{
				Package: backendModulePath + "/internal/repository/memory",
				ShouldNotDependsOn: internalDeps(
					backendModulePath+"/internal/http",
					backendModulePath+"/internal/http/handler",
					backendModulePath+"/internal/service",
					backendModulePath+"/cmd/server",
					backendModulePath+"/cmd/worker",
				),
			},
		},
	})

	if result.Pass {
		return
	}

	t.Fatalf("backend architecture guardrails failed:\n%s", formatDependencyViolations(result))
}

func internalDeps(patterns ...string) *configuration.Dependencies {
	return &configuration.Dependencies{Internal: patterns}
}

func formatDependencyViolations(result *archgo.Result) string {
	if result == nil || result.DependenciesRuleResult == nil {
		return "no dependency result details available"
	}

	var builder strings.Builder
	for _, ruleResult := range result.DependenciesRuleResult.Results {
		if ruleResult == nil || ruleResult.Passes {
			continue
		}

		builder.WriteString("- ")
		builder.WriteString(ruleResult.Description)
		builder.WriteString("\n")

		for _, verification := range ruleResult.Verifications {
			if verification.Passes {
				continue
			}

			builder.WriteString("  package: ")
			builder.WriteString(verification.Package)
			builder.WriteString("\n")
			for _, detail := range verification.Details {
				builder.WriteString("    - ")
				builder.WriteString(detail)
				builder.WriteString("\n")
			}
		}
	}

	if builder.Len() == 0 {
		return "dependency rules failed without violation details"
	}

	return strings.TrimRight(builder.String(), "\n")
}
