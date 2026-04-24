package versioning

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type Strategy string

const (
	ReleaseStrategyManual      Strategy = "manual"
	ReleaseStrategySemverPatch Strategy = "semver-patch"
	ReleaseStrategyTemplate    Strategy = "template"
)

type Config struct {
	Strategy string
	Version  string
	Template string
}

type ResolveInput struct {
	Config           Config
	Build            domain.Build
	ExistingVersions []string
}

type semverSeries struct {
	Major int
	Minor int
}

var releaseTemplatePattern = regexp.MustCompile(`\{([a-z_]+)\}`)

func NormalizeStrategy(value string) Strategy {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", string(ReleaseStrategyManual):
		return ReleaseStrategyManual
	case string(ReleaseStrategySemverPatch):
		return ReleaseStrategySemverPatch
	case string(ReleaseStrategyTemplate):
		return ReleaseStrategyTemplate
	default:
		return Strategy(strings.TrimSpace(strings.ToLower(value)))
	}
}

func (c Config) Empty() bool {
	return strings.TrimSpace(c.Strategy) == "" && strings.TrimSpace(c.Version) == "" && strings.TrimSpace(c.Template) == ""
}

func ValidateConfig(config Config) error {
	switch NormalizeStrategy(config.Strategy) {
	case ReleaseStrategyManual:
		if strings.TrimSpace(config.Version) == "" {
			return fmt.Errorf("release.version is required for manual strategy")
		}
		return nil
	case ReleaseStrategySemverPatch:
		_, err := parseSemverSeries(config.Version)
		if err != nil {
			return err
		}
		return nil
	case ReleaseStrategyTemplate:
		return validateTemplate(config.Template)
	default:
		return fmt.Errorf("release.strategy must be one of manual, semver-patch, or template")
	}
}

func ResolveVersion(input ResolveInput) (string, error) {
	if err := ValidateConfig(input.Config); err != nil {
		return "", err
	}

	switch NormalizeStrategy(input.Config.Strategy) {
	case ReleaseStrategyManual:
		return strings.TrimSpace(input.Config.Version), nil
	case ReleaseStrategySemverPatch:
		series, err := parseSemverSeries(input.Config.Version)
		if err != nil {
			return "", err
		}
		return resolveNextPatch(series, input.ExistingVersions), nil
	case ReleaseStrategyTemplate:
		return renderTemplate(strings.TrimSpace(input.Config.Template), input.Build)
	default:
		return "", fmt.Errorf("unsupported release strategy %q", input.Config.Strategy)
	}
}

func parseSemverSeries(value string) (semverSeries, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return semverSeries{}, fmt.Errorf("release.version is required for semver-patch strategy")
	}
	parts := strings.Split(trimmed, ".")
	if len(parts) != 2 {
		return semverSeries{}, fmt.Errorf("release.version for semver-patch strategy must be major.minor")
	}
	major, err := parseReleasePart(parts[0])
	if err != nil {
		return semverSeries{}, err
	}
	minor, err := parseReleasePart(parts[1])
	if err != nil {
		return semverSeries{}, err
	}
	return semverSeries{Major: major, Minor: minor}, nil
}

func resolveNextPatch(series semverSeries, existing []string) string {
	nextPatch := 0
	for _, candidate := range existing {
		parts := strings.Split(strings.TrimSpace(candidate), ".")
		if len(parts) != 3 {
			continue
		}
		major, majorErr := parseReleasePart(parts[0])
		minor, minorErr := parseReleasePart(parts[1])
		patch, patchErr := parseReleasePart(parts[2])
		if majorErr != nil || minorErr != nil || patchErr != nil {
			continue
		}
		if major != series.Major || minor != series.Minor {
			continue
		}
		if patch >= nextPatch {
			nextPatch = patch + 1
		}
	}
	return fmt.Sprintf("%d.%d.%d", series.Major, series.Minor, nextPatch)
}

func validateTemplate(value string) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("release.template is required for template strategy")
	}
	matches := releaseTemplatePattern.FindAllStringSubmatchIndex(trimmed, -1)
	if len(matches) == 0 {
		return nil
	}
	lastIndex := 0
	for _, match := range matches {
		if strings.ContainsAny(trimmed[lastIndex:match[0]], "{}") {
			return fmt.Errorf("release.template contains malformed placeholders")
		}
		name := trimmed[match[2]:match[3]]
		if !isSupportedPlaceholder(name) {
			return fmt.Errorf("release.template placeholder %q is not supported", name)
		}
		lastIndex = match[1]
	}
	if strings.ContainsAny(trimmed[lastIndex:], "{}") {
		return fmt.Errorf("release.template contains malformed placeholders")
	}
	return nil
}

func renderTemplate(template string, build domain.Build) (string, error) {
	if err := validateTemplate(template); err != nil {
		return "", err
	}
	resolved := releaseTemplatePattern.ReplaceAllStringFunc(template, func(token string) string {
		name := token[1 : len(token)-1]
		return releasePlaceholderValue(name, build)
	})
	return strings.TrimSpace(resolved), nil
}

func isSupportedPlaceholder(name string) bool {
	switch name {
	case "build_number", "attempt_number", "commit_sha", "short_commit_sha":
		return true
	default:
		return false
	}
}

func releasePlaceholderValue(name string, build domain.Build) string {
	switch name {
	case "build_number":
		if build.BuildNumber <= 0 {
			return ""
		}
		return strconv.FormatInt(build.BuildNumber, 10)
	case "attempt_number":
		attempt := build.AttemptNumber
		if attempt <= 0 {
			attempt = 1
		}
		return strconv.Itoa(attempt)
	case "commit_sha":
		if build.CommitSHA != nil {
			return strings.TrimSpace(*build.CommitSHA)
		}
		if build.Source != nil && build.Source.CommitSHA != nil {
			return strings.TrimSpace(*build.Source.CommitSHA)
		}
		return ""
	case "short_commit_sha":
		value := releasePlaceholderValue("commit_sha", build)
		if len(value) > 8 {
			return value[:8]
		}
		return value
	default:
		return ""
	}
}

func parseReleasePart(value string) (int, error) {
	if value == "" {
		return 0, fmt.Errorf("release version segments must be numeric")
	}
	if len(value) > 1 && strings.HasPrefix(value, "0") {
		return 0, fmt.Errorf("release version segments must not contain leading zeroes")
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("release version segments must be numeric")
	}
	return parsed, nil
}
