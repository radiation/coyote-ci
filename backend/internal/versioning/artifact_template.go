package versioning

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

var artifactTemplatePattern = regexp.MustCompile(`\{([a-z_]+)\}`)

func ValidateArtifactVersionConfig(template string, channel string) error {
	trimmedTemplate := strings.TrimSpace(template)
	trimmedChannel := strings.TrimSpace(channel)
	if trimmedTemplate == "" && trimmedChannel == "" {
		return fmt.Errorf("artifact version template or channel is required")
	}
	if trimmedTemplate == "" && trimmedChannel != "" {
		return fmt.Errorf("artifact version channel requires a template")
	}
	return ValidateArtifactVersionTemplate(trimmedTemplate)
}

func ValidateArtifactVersionTemplate(template string) error {
	trimmed := strings.TrimSpace(template)
	if trimmed == "" {
		return fmt.Errorf("artifact version template is required")
	}
	matches := artifactTemplatePattern.FindAllStringSubmatchIndex(trimmed, -1)
	if len(matches) == 0 {
		return nil
	}
	lastIndex := 0
	for _, match := range matches {
		if strings.ContainsAny(trimmed[lastIndex:match[0]], "{}") {
			return fmt.Errorf("artifact version template contains malformed placeholders")
		}
		name := trimmed[match[2]:match[3]]
		if !isSupportedArtifactTemplatePlaceholder(name) {
			return fmt.Errorf("artifact version template placeholder %q is not supported", name)
		}
		lastIndex = match[1]
	}
	if strings.ContainsAny(trimmed[lastIndex:], "{}") {
		return fmt.Errorf("artifact version template contains malformed placeholders")
	}
	return nil
}

func ResolveArtifactVersionTemplate(template string, build domain.Build) (string, error) {
	trimmed := strings.TrimSpace(template)
	if err := ValidateArtifactVersionTemplate(trimmed); err != nil {
		return "", err
	}
	var resolveErr error
	resolved := artifactTemplatePattern.ReplaceAllStringFunc(trimmed, func(token string) string {
		if resolveErr != nil {
			return ""
		}
		name := token[1 : len(token)-1]
		value, err := artifactTemplatePlaceholderValue(name, build)
		if err != nil {
			resolveErr = err
			return ""
		}
		return value
	})
	if resolveErr != nil {
		return "", resolveErr
	}
	return strings.TrimSpace(resolved), nil
}

func isSupportedArtifactTemplatePlaceholder(name string) bool {
	switch name {
	case "build_number", "git_sha", "git_short_sha", "git_ref":
		return true
	default:
		return false
	}
}

func artifactTemplatePlaceholderValue(name string, build domain.Build) (string, error) {
	switch name {
	case "build_number":
		if build.BuildNumber <= 0 {
			return "", fmt.Errorf("artifact version template token {build_number} requires build number metadata")
		}
		return strconv.FormatInt(build.BuildNumber, 10), nil
	case "git_sha":
		sha := artifactTemplateGitSHA(build)
		if sha == "" {
			return "", fmt.Errorf("artifact version template token {git_sha} requires git sha metadata")
		}
		return sha, nil
	case "git_short_sha":
		sha := artifactTemplateGitSHA(build)
		if sha == "" {
			return "", fmt.Errorf("artifact version template token {git_short_sha} requires git sha metadata")
		}
		if len(sha) > 8 {
			return sha[:8], nil
		}
		return sha, nil
	case "git_ref":
		ref := artifactTemplateGitRef(build)
		if ref == "" {
			return "", fmt.Errorf("artifact version template token {git_ref} requires git ref metadata")
		}
		return ref, nil
	default:
		return "", fmt.Errorf("artifact version template placeholder %q is not supported", name)
	}
}

func artifactTemplateGitSHA(build domain.Build) string {
	for _, value := range []string{
		optionalString(build.SourceSHA),
		optionalString(build.CommitSHA),
		optionalSourceString(build.Source, true),
		optionalString(build.Trigger.CommitSHA),
	} {
		if value != "" {
			return value
		}
	}
	return ""
}

func artifactTemplateGitRef(build domain.Build) string {
	for _, value := range []string{
		optionalString(build.SourceRef),
		optionalString(build.Ref),
		optionalSourceString(build.Source, false),
		optionalString(build.Trigger.Ref),
	} {
		if value != "" {
			return value
		}
	}
	return ""
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func optionalSourceString(source *domain.SourceSpec, wantCommit bool) string {
	if source == nil {
		return ""
	}
	if wantCommit {
		return optionalString(source.CommitSHA)
	}
	return optionalString(source.Ref)
}
