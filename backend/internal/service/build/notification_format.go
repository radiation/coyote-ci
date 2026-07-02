package build

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

type buildNotificationDetails struct {
	statusSummary string
	projectID     string
	projectName   string
	projectLabel  string
	projectURL    string
	jobID         string
	jobName       string
	jobLabel      string
	jobURL        string
	buildID       string
	buildNumber   int64
	buildLabel    string
	durationLabel string
	refLabel      string
	shaFull       string
	shaLabel      string
	commitURL     string
	authorName    string
	authorEmail   string
	authorLabel   string
	buildURL      string
}

var fullNotificationSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func (s *BuildNotificationService) buildNotificationDetails(ctx context.Context, build domain.Build) buildNotificationDetails {
	details := buildNotificationDetails{
		statusSummary: buildStatusNotificationSummary(build.Status),
		projectID:     strings.TrimSpace(build.ProjectID),
		projectName:   strings.TrimSpace(build.ProjectID),
		projectLabel:  build.ProjectID,
		buildID:       strings.TrimSpace(build.ID),
		buildNumber:   build.BuildNumber,
		buildLabel:    build.ID,
	}
	repositoryRemote := resolveBuildRepositoryRemote(build)
	if s.projectRepo != nil && strings.TrimSpace(build.ProjectID) != "" {
		project, err := s.projectRepo.GetByID(ctx, build.ProjectID)
		if err == nil && strings.TrimSpace(project.Name) != "" {
			details.projectName = strings.TrimSpace(project.Name)
			details.projectLabel = fmt.Sprintf("%s (%s)", project.Name, project.ID)
		}
	}

	jobLabel := ""
	if build.JobID != nil {
		jobID := strings.TrimSpace(*build.JobID)
		if jobID != "" {
			details.jobID = jobID
			jobLabel = jobID
			details.jobName = jobID
			if s.jobRepo != nil {
				job, err := s.jobRepo.GetByID(ctx, jobID)
				if err == nil {
					if strings.TrimSpace(job.RepositoryURL) != "" && repositoryRemote == "" {
						repositoryRemote = strings.TrimSpace(job.RepositoryURL)
					}
					if strings.TrimSpace(job.Name) != "" {
						details.jobName = strings.TrimSpace(job.Name)
						jobLabel = fmt.Sprintf("%s (%s)", job.Name, job.ID)
					}
				}
			}
		}
	}
	details.jobLabel = jobLabel
	if build.BuildNumber > 0 {
		details.buildLabel = fmt.Sprintf("#%d (%s)", build.BuildNumber, build.ID)
	}
	if build.StartedAt != nil && build.FinishedAt != nil && !build.StartedAt.IsZero() && !build.FinishedAt.IsZero() && build.FinishedAt.After(*build.StartedAt) {
		details.durationLabel = build.FinishedAt.Sub(*build.StartedAt).Round(time.Second).String()
	}
	if build.SourceRef != nil && strings.TrimSpace(*build.SourceRef) != "" {
		details.refLabel = strings.TrimSpace(*build.SourceRef)
	} else if build.Ref != nil && strings.TrimSpace(*build.Ref) != "" {
		details.refLabel = strings.TrimSpace(*build.Ref)
	}
	if build.SourceSHA != nil && strings.TrimSpace(*build.SourceSHA) != "" {
		details.shaFull = strings.TrimSpace(*build.SourceSHA)
		details.shaLabel = shortNotificationSHA(*build.SourceSHA)
	} else if build.CommitSHA != nil && strings.TrimSpace(*build.CommitSHA) != "" {
		details.shaFull = strings.TrimSpace(*build.CommitSHA)
		details.shaLabel = shortNotificationSHA(*build.CommitSHA)
	}
	details.authorName = trimNotificationOptionalString(build.SourceAuthorName)
	details.authorEmail = trimNotificationOptionalString(build.SourceAuthorEmail)
	details.authorLabel = formatNotificationAuthor(build)
	if s.publicBaseURL != "" {
		details.projectURL = buildProjectDetailURL(s.publicBaseURL, details.projectID)
		details.jobURL = buildJobDetailURL(s.publicBaseURL, details.jobID)
		details.buildURL = buildBuildDetailURL(s.publicBaseURL, details.buildID)
	}
	if details.shaFull != "" && repositoryRemote != "" {
		commitURL, ok := buildRepositoryCommitURL(repositoryRemote, details.shaFull)
		if ok {
			details.commitURL = commitURL
		}
	}
	return details
}

func (s *BuildNotificationService) formatBuildStatusEmail(build domain.Build, details buildNotificationDetails) (string, string) {
	subjectParts := []string{"Coyote CI", "build", details.statusSummary, build.ID}
	if details.jobLabel != "" {
		subjectParts = []string{"Coyote CI", details.jobLabel, "build", details.statusSummary, build.ID}
	}
	subject := strings.Join(subjectParts, " ")

	bodyLines := []string{
		fmt.Sprintf("A Coyote CI build %s.", details.statusSummary),
		"",
		fmt.Sprintf("Build ID: %s", build.ID),
		fmt.Sprintf("Status: %s", build.Status),
		fmt.Sprintf("Project: %s", details.projectLabel),
	}
	if details.projectURL != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Project detail: %s", details.projectURL))
	}
	if build.BuildNumber > 0 {
		bodyLines = append(bodyLines, fmt.Sprintf("Build number: %d", build.BuildNumber))
	}
	if details.jobLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Job: %s", details.jobLabel))
		if details.jobURL != "" {
			bodyLines = append(bodyLines, fmt.Sprintf("Job detail: %s", details.jobURL))
		}
	}
	if details.durationLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Duration: %s", details.durationLabel))
	}
	if details.refLabel != "" || details.shaLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Git: %s", joinNotificationGitParts(details.refLabel, details.shaLabel)))
	}
	if details.authorLabel != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Commit author: %s", details.authorLabel))
	}
	if details.buildURL != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Build detail: %s", details.buildURL))
	}
	if build.ErrorMessage != nil && strings.TrimSpace(*build.ErrorMessage) != "" {
		bodyLines = append(bodyLines, fmt.Sprintf("Error: %s", strings.TrimSpace(*build.ErrorMessage)))
	}

	return subject, strings.Join(bodyLines, "\n")
}

func formatBuildStatusSlackText(details buildNotificationDetails) string {
	lines := []string{fmt.Sprintf("%s Build %s", slackStatusIndicator(details.statusSummary), details.statusSummary)}
	if details.projectLabel != "" {
		projectText := slackEscapeMrkdwnLabel(details.projectLabel)
		if details.projectURL != "" {
			label := strings.TrimSpace(details.projectName)
			if label == "" {
				label = details.projectLabel
			}
			projectText = slackMrkdwnLink(details.projectURL, label)
		}
		lines = append(lines, fmt.Sprintf("Project: %s", projectText))
	}
	if details.jobLabel != "" {
		jobText := slackEscapeMrkdwnLabel(details.jobLabel)
		if details.jobURL != "" {
			label := strings.TrimSpace(details.jobName)
			if label == "" {
				label = details.jobLabel
			}
			jobText = slackMrkdwnLink(details.jobURL, label)
		}
		lines = append(lines, fmt.Sprintf("Job: %s", jobText))
	}
	if details.buildLabel != "" {
		buildText := slackEscapeMrkdwnLabel(details.buildLabel)
		if details.buildURL != "" {
			label := details.buildLabel
			if details.buildNumber > 0 {
				label = fmt.Sprintf("#%d", details.buildNumber)
			}
			buildText = slackMrkdwnLink(details.buildURL, label)
		}
		lines = append(lines, fmt.Sprintf("Build: %s", buildText))
	}
	if details.durationLabel != "" {
		lines = append(lines, fmt.Sprintf("Duration: %s", details.durationLabel))
	}
	if details.refLabel != "" || details.shaLabel != "" {
		gitLabel := joinNotificationGitParts(details.refLabel, details.shaLabel)
		gitText := slackEscapeMrkdwnLabel(gitLabel)
		if details.commitURL != "" {
			linkedRefLabel := slackGitRefLabel(details.refLabel)
			linkedGitLabel := joinNotificationGitParts(linkedRefLabel, details.shaLabel)
			if linkedGitLabel == "" {
				linkedGitLabel = gitLabel
			}
			gitText = slackMrkdwnLink(details.commitURL, linkedGitLabel)
		}
		lines = append(lines, fmt.Sprintf("Git: %s", gitText))
	}
	authorText := formatNotificationAuthorSlack(details.authorName, details.authorEmail)
	if authorText != "" {
		lines = append(lines, fmt.Sprintf("Commit author: %s", authorText))
	}
	if details.buildURL != "" {
		lines = append(lines, fmt.Sprintf("Build detail: %s", details.buildURL))
	}
	return strings.Join(lines, "\n")
}

func formatPersonalBuildStatusSlackText(details buildNotificationDetails) string {
	statusLabel := "Build completed"
	switch details.statusSummary {
	case "failed":
		statusLabel = "Build failed"
	case "succeeded":
		statusLabel = "Build succeeded"
	}

	contextParts := make([]string, 0, 3)
	if strings.TrimSpace(details.projectName) != "" {
		contextParts = append(contextParts, strings.TrimSpace(details.projectName))
	}
	if strings.TrimSpace(details.jobName) != "" {
		contextParts = append(contextParts, strings.TrimSpace(details.jobName))
	}
	if details.buildNumber > 0 {
		contextParts = append(contextParts, fmt.Sprintf("#%d", details.buildNumber))
	} else if strings.TrimSpace(details.buildID) != "" {
		contextParts = append(contextParts, strings.TrimSpace(details.buildID))
	}

	headline := statusLabel
	if len(contextParts) > 0 {
		headline = fmt.Sprintf("%s: %s", statusLabel, strings.Join(contextParts, " / "))
	}

	lines := []string{headline}
	if details.shaLabel != "" {
		lines = append(lines, fmt.Sprintf("Commit: %s", details.shaLabel))
	}
	if details.refLabel != "" {
		lines = append(lines, fmt.Sprintf("Ref: %s", details.refLabel))
	}
	if details.buildURL != "" {
		lines = append(lines, fmt.Sprintf("View build: %s", details.buildURL))
	}
	if details.statusSummary == "failed" && details.durationLabel != "" {
		lines = append(lines, fmt.Sprintf("Duration: %s", details.durationLabel))
	}
	return strings.Join(lines, "\n")
}

func slackStatusIndicator(statusSummary string) string {
	switch statusSummary {
	case "succeeded":
		return ":white_check_mark:"
	case "failed":
		return ":x:"
	default:
		return ":information_source:"
	}
}

func shortNotificationSHA(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= 7 {
		return trimmed
	}
	return trimmed[:7]
}

func formatNotificationAuthor(build domain.Build) string {
	name := trimNotificationOptionalString(build.SourceAuthorName)
	email := trimNotificationOptionalString(build.SourceAuthorEmail)
	if name == "" {
		return email
	}
	if email == "" {
		return name
	}
	return fmt.Sprintf("%s <%s>", name, email)
}

func joinNotificationGitParts(refLabel string, shaLabel string) string {
	if refLabel == "" {
		return shaLabel
	}
	if shaLabel == "" {
		return refLabel
	}
	return refLabel + " @ " + shaLabel
}

func slackGitRefLabel(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if strings.HasPrefix(trimmed, "refs/heads/") {
		return strings.TrimPrefix(trimmed, "refs/heads/")
	}
	if strings.HasPrefix(trimmed, "refs/tags/") {
		return strings.TrimPrefix(trimmed, "refs/tags/")
	}
	return trimmed
}

func buildProjectDetailURL(publicBaseURL string, projectID string) string {
	return buildFrontendEntityURL(publicBaseURL, "/projects/", projectID)
}

func buildJobDetailURL(publicBaseURL string, jobID string) string {
	return buildFrontendEntityURL(publicBaseURL, "/jobs/", jobID)
}

func buildBuildDetailURL(publicBaseURL string, buildID string) string {
	return buildFrontendEntityURL(publicBaseURL, "/builds/", buildID)
}

func buildFrontendEntityURL(publicBaseURL string, pathPrefix string, id string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	trimmedID := strings.TrimSpace(id)
	if base == "" || trimmedID == "" {
		return ""
	}
	return base + pathPrefix + url.PathEscape(trimmedID)
}

func slackEscapeMrkdwnLabel(label string) string {
	replacer := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(label)
}

func slackMrkdwnLink(linkURL string, label string) string {
	trimmedURL := strings.TrimSpace(linkURL)
	if trimmedURL == "" {
		return slackEscapeMrkdwnLabel(label)
	}
	return fmt.Sprintf("<%s|%s>", trimmedURL, slackEscapeMrkdwnLabel(label))
}

func formatNotificationAuthorSlack(name string, email string) string {
	trimmedName := strings.TrimSpace(name)
	trimmedEmail := strings.TrimSpace(email)
	if trimmedName == "" {
		return slackEscapeMrkdwnLabel(trimmedEmail)
	}
	if trimmedEmail == "" {
		return slackEscapeMrkdwnLabel(trimmedName)
	}
	return fmt.Sprintf("%s (%s)", slackEscapeMrkdwnLabel(trimmedName), slackEscapeMrkdwnLabel(trimmedEmail))
}

func resolveBuildRepositoryRemote(build domain.Build) string {
	if build.Source != nil && strings.TrimSpace(build.Source.RepositoryURL) != "" {
		return strings.TrimSpace(build.Source.RepositoryURL)
	}
	if build.RepoURL != nil && strings.TrimSpace(*build.RepoURL) != "" {
		return strings.TrimSpace(*build.RepoURL)
	}
	if build.Trigger.RepositoryURL != nil && strings.TrimSpace(*build.Trigger.RepositoryURL) != "" {
		return strings.TrimSpace(*build.Trigger.RepositoryURL)
	}
	return ""
}

func buildRepositoryCommitURL(repositoryRemote string, fullSHA string) (string, bool) {
	trimmedSHA := strings.TrimSpace(fullSHA)
	if !fullNotificationSHAPattern.MatchString(trimmedSHA) {
		return "", false
	}

	host, owner, repo, ok := parseRepositoryRemote(repositoryRemote)
	if !ok {
		return "", false
	}
	if host != "github.com" {
		return "", false
	}

	return fmt.Sprintf("https://%s/%s/%s/commit/%s", host, owner, repo, trimmedSHA), true
}

func parseRepositoryRemote(repositoryRemote string) (host string, owner string, repo string, ok bool) {
	trimmedRemote := strings.TrimSpace(repositoryRemote)
	if trimmedRemote == "" {
		return "", "", "", false
	}

	if strings.HasPrefix(trimmedRemote, "git@") {
		withoutPrefix := strings.TrimPrefix(trimmedRemote, "git@")
		parts := strings.SplitN(withoutPrefix, ":", 2)
		if len(parts) != 2 {
			return "", "", "", false
		}
		return parseRemoteHostAndPath(parts[0], parts[1])
	}

	parsed, err := url.Parse(trimmedRemote)
	if err != nil {
		return "", "", "", false
	}
	if parsed.Hostname() == "" {
		return "", "", "", false
	}

	return parseRemoteHostAndPath(parsed.Hostname(), parsed.Path)
}

func parseRemoteHostAndPath(hostValue string, pathValue string) (host string, owner string, repo string, ok bool) {
	host = strings.ToLower(strings.TrimSpace(hostValue))
	trimmedPath := strings.Trim(strings.TrimSpace(pathValue), "/")
	segments := strings.Split(trimmedPath, "/")
	if host == "" || len(segments) < 2 {
		return "", "", "", false
	}

	owner = strings.TrimSpace(segments[0])
	repo = strings.TrimSuffix(strings.TrimSpace(segments[1]), ".git")
	if owner == "" || repo == "" {
		return "", "", "", false
	}
	return host, owner, repo, true
}

func trimNotificationOptionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
