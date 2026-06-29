package architecture_test

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	archgo "github.com/arch-go/arch-go/v2/api"
	"github.com/arch-go/arch-go/v2/api/configuration"
	"golang.org/x/tools/go/packages"
)

const backendModulePath = "github.com/radiation/coyote-ci/backend"

func TestBackendArchitectureGuardrails(t *testing.T) {
	t.Helper()

	moduleInfo := configuration.Load(backendModulePath)
	result := archgo.CheckArchitecture(moduleInfo, configuration.Config{
		DependenciesRules: backendDependencyRules(),
	})

	if result.Pass {
		return
	}

	t.Fatalf("backend architecture guardrails failed:\n%s", formatDependencyViolations(result))
}

func TestConcreteRepositoryImportsStayInCompositionRoots(t *testing.T) {
	t.Helper()

	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedModule,
		Dir:  backendModuleRoot(t),
	}
	pkgs, err := packages.Load(config, "./...")
	if err != nil {
		t.Fatalf("load backend packages: %v", err)
	}
	if packageErrors := packages.PrintErrors(pkgs); packageErrors > 0 {
		t.Fatalf("encountered %d package loading errors", packageErrors)
	}

	allowedImporters := map[string]struct{}{
		backendModulePath + "/cmd/server": {},
		backendModulePath + "/cmd/worker": {},
	}
	concreteAdapters := map[string]struct{}{
		backendModulePath + "/internal/repository/postgres": {},
		backendModulePath + "/internal/repository/memory":   {},
	}

	violations := make([]string, 0)
	for _, pkg := range pkgs {
		if pkg == nil || pkg.PkgPath == "" {
			continue
		}

		for importPath := range pkg.Imports {
			if !isConcreteAdapterImport(importPath, concreteAdapters) {
				continue
			}
			if _, allowed := allowedImporters[pkg.PkgPath]; allowed {
				continue
			}

			violations = append(violations, fmt.Sprintf("%s imports %s", pkg.PkgPath, importPath))
		}
	}

	if len(violations) == 0 {
		return
	}

	slices.Sort(violations)
	t.Fatalf("concrete repositories must only be imported by composition roots:\n- %s", strings.Join(violations, "\n- "))
}

func backendDependencyRules() []*configuration.DependenciesRule {
	handlerAndCommandDeps := []string{
		backendModulePath + "/internal/http",
		backendModulePath + "/internal/http/handler",
		backendModulePath + "/cmd/server",
		backendModulePath + "/cmd/worker",
	}
	concreteRepositoryDeps := []string{
		backendModulePath + "/internal/repository/postgres",
		backendModulePath + "/internal/repository/memory",
	}
	serviceDeps := []string{backendModulePath + "/internal/service"}
	lowLevelUpwardDeps := []string{
		backendModulePath + "/internal/http",
		backendModulePath + "/internal/http/handler",
		backendModulePath + "/cmd/server",
		backendModulePath + "/cmd/worker",
	}

	rules := make([]*configuration.DependenciesRule, 0, 16)

	// Paired exact and descendant rules intentionally cover both the base package
	// and future nested packages without forcing cross-cutting packages into a layer.
	rules = append(rules, exactAndDescendantRules(
		backendModulePath+"/internal/domain",
		append(append([]string{}, handlerAndCommandDeps...), append(serviceDeps, concreteRepositoryDeps...)...)...,
	)...)
	rules = append(rules, exactAndDescendantRules(
		backendModulePath+"/internal/http/handler",
		append(append([]string{}, concreteRepositoryDeps...), backendModulePath+"/cmd/server", backendModulePath+"/cmd/worker")...,
	)...)
	rules = append(rules, exactAndDescendantRules(
		backendModulePath+"/internal/service",
		append(append([]string{}, handlerAndCommandDeps...), concreteRepositoryDeps...)...,
	)...)
	rules = append(rules, dependencyRule(
		backendModulePath+"/internal/repository",
		append(append([]string{}, concreteRepositoryDeps...), append(handlerAndCommandDeps, backendModulePath+"/internal/service")...)...,
	))
	rules = append(rules, exactAndDescendantRules(
		backendModulePath+"/internal/repository/postgres",
		append(append([]string{}, handlerAndCommandDeps...), backendModulePath+"/internal/service")...,
	)...)
	rules = append(rules, exactAndDescendantRules(
		backendModulePath+"/internal/repository/memory",
		append(append([]string{}, handlerAndCommandDeps...), backendModulePath+"/internal/service")...,
	)...)
	rules = append(rules, exactAndDescendantRules(
		backendModulePath+"/internal/observability",
		lowLevelUpwardDeps...,
	)...)
	rules = append(rules, exactAndDescendantRules(
		backendModulePath+"/internal/platform",
		lowLevelUpwardDeps...,
	)...)
	rules = append(rules, dependencyRule(
		backendModulePath+"/internal/logs",
		lowLevelUpwardDeps...,
	))

	return rules
}

func isConcreteAdapterImport(importPath string, concreteAdapters map[string]struct{}) bool {
	for adapterPath := range concreteAdapters {
		if importPath == adapterPath || strings.HasPrefix(importPath, adapterPath+"/") {
			return true
		}
	}

	return false
}

func exactAndDescendantRules(packagePath string, disallowed ...string) []*configuration.DependenciesRule {
	return []*configuration.DependenciesRule{
		dependencyRule(packagePath, disallowed...),
		dependencyRule(packagePath+".**", disallowed...),
	}
}

func dependencyRule(packagePattern string, disallowed ...string) *configuration.DependenciesRule {
	return &configuration.DependenciesRule{
		Package:            packagePattern,
		ShouldNotDependsOn: internalDeps(disallowed...),
	}
}

func internalDeps(patterns ...string) *configuration.Dependencies {
	return &configuration.Dependencies{Internal: patterns}
}

func backendModuleRoot(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test file path")
	}

	return filepath.Dir(filepath.Dir(filename))
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
