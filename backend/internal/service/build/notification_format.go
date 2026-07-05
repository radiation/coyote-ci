package build

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/domain"
)

const maxNotificationFailureMessageLength = 160
const maxNotificationArtifactLabelLength = 80
const maxNotificationArtifactLinks = 3

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
	failedStep    *buildNotificationStep
	failureText   string
	failureExit   *int
	diagnosticURL string
	artifactsURL  string
	artifacts     []notificationArtifactLink
	artifactCount int
}

type buildNotificationStep struct {
	index int
	name  string
	label string
	url   string
}

type notificationArtifactLink struct {
	label string
	url   string
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

func (s *BuildNotificationService) enrichSlackNotificationDetails(ctx context.Context, build domain.Build, details buildNotificationDetails) (buildNotificationDetails, error) {
	details = applyFallbackFailureContext(build, details)
	if details.statusSummary == "failed" {
		enriched, err := s.enrichFailedSlackNotificationDetails(ctx, build, details)
		if err != nil {
			return buildNotificationDetails{}, err
		}
		return applyFallbackFailureContext(build, enriched), nil
	}
	if details.statusSummary == "succeeded" {
		enriched, err := s.enrichSuccessfulSlackNotificationDetails(ctx, build, details)
		if err != nil {
			return buildNotificationDetails{}, err
		}
		return enriched, nil
	}
	return details, nil
}

func (s *BuildNotificationService) enrichFailedSlackNotificationDetails(ctx context.Context, build domain.Build, details buildNotificationDetails) (buildNotificationDetails, error) {
	if strings.TrimSpace(details.buildID) == "" {
		return details, nil
	}
	if s.buildRepo == nil {
		return details, nil
	}
	steps, err := s.buildRepo.GetStepsByBuildID(ctx, details.buildID)
	if err != nil {
		return buildNotificationDetails{}, retryableNotificationExecutionFailure("notification_step_enrichment_failed", "notification failed-step enrichment failed", err)
	}
	details.failedStep = buildNotificationFailedStep(s.publicBaseURL, details.buildID, steps)
	if details.failedStep != nil {
		if errorMessage := strings.TrimSpace(trimNotificationOptionalString(stepErrorPointer(steps, details.failedStep.index))); errorMessage != "" {
			details.failureText = truncateNotificationText(errorMessage, maxNotificationFailureMessageLength)
		}
		details.failureExit = stepExitCodePointer(steps, details.failedStep.index)
		details.diagnosticURL = details.failedStep.url
	}
	return details, nil
}

func (s *BuildNotificationService) enrichSuccessfulSlackNotificationDetails(ctx context.Context, build domain.Build, details buildNotificationDetails) (buildNotificationDetails, error) {
	if s.publicBaseURL == "" || strings.TrimSpace(details.buildID) == "" {
		return details, nil
	}
	if s.artifactRepo == nil {
		return details, nil
	}
	artifacts, err := s.artifactRepo.ListByBuildID(ctx, details.buildID)
	if err != nil {
		return buildNotificationDetails{}, retryableNotificationExecutionFailure("notification_artifact_enrichment_failed", "notification artifact enrichment failed", err)
	}
	details.artifactCount = len(artifacts)
	details.artifactsURL = buildArtifactsListURL(s.publicBaseURL, details.buildID)
	details.artifacts = buildNotificationArtifactLinks(s.publicBaseURL, artifacts)
	return details, nil
}

func applyFallbackFailureContext(build domain.Build, details buildNotificationDetails) buildNotificationDetails {
	if details.failureText == "" && build.ErrorMessage != nil {
		details.failureText = truncateNotificationText(strings.TrimSpace(*build.ErrorMessage), maxNotificationFailureMessageLength)
	}
	if details.diagnosticURL == "" {
		details.diagnosticURL = details.buildURL
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
	lines := []string{formatBuildStatusHeadline(details)}
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
			buildText = slackMrkdwnLink(details.buildURL, notificationBuildLinkLabel(details))
		}
		lines = append(lines, fmt.Sprintf("Build: %s", buildText))
	}
	if details.refLabel != "" || details.shaLabel != "" {
		lines = append(lines, fmt.Sprintf("Commit: %s", notificationSlackCommitText(details)))
	}
	if details.statusSummary == "failed" {
		if details.failedStep != nil {
			lines = append(lines, fmt.Sprintf("Failed step: %s", notificationStepSlackText(*details.failedStep)))
		}
		if details.failureText != "" {
			lines = append(lines, fmt.Sprintf("Reason: %s", slackEscapeMrkdwnLabel(details.failureText)))
		}
		if details.failureExit != nil {
			lines = append(lines, fmt.Sprintf("Exit code: %d", *details.failureExit))
		}
	}
	if details.statusSummary == "failed" {
		authorText := formatNotificationAuthorSlack(details.authorName, details.authorEmail)
		if authorText != "" {
			lines = append(lines, fmt.Sprintf("Author: %s", authorText))
		}
	}
	if details.durationLabel != "" {
		lines = append(lines, fmt.Sprintf("Duration: %s", details.durationLabel))
	}
	if details.statusSummary == "failed" && details.diagnosticURL != "" {
		lines = append(lines, fmt.Sprintf("Diagnostic: %s", slackMrkdwnLink(details.diagnosticURL, notificationDiagnosticLabel(details))))
	}
	if details.statusSummary == "succeeded" {
		artifactLine := formatNotificationArtifactSlackLine(details)
		if artifactLine != "" {
			lines = append(lines, artifactLine)
		}
	}
	lines = append(lines, notificationSlackCLIHintLines(details)...)
	if details.buildURL != "" {
		lines = append(lines, fmt.Sprintf("Build details: %s", slackMrkdwnLink(details.buildURL, "View build")))
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
	if details.refLabel != "" || details.shaLabel != "" {
		lines = append(lines, fmt.Sprintf("Commit: %s", notificationSlackCommitText(details)))
	}
	if details.statusSummary == "failed" {
		if details.failedStep != nil {
			lines = append(lines, fmt.Sprintf("Failed step: %s", notificationStepSlackText(*details.failedStep)))
		}
		if details.failureText != "" {
			lines = append(lines, fmt.Sprintf("Reason: %s", slackEscapeMrkdwnLabel(details.failureText)))
		}
		if details.failureExit != nil {
			lines = append(lines, fmt.Sprintf("Exit code: %d", *details.failureExit))
		}
		authorText := formatNotificationAuthorSlack(details.authorName, details.authorEmail)
		if authorText != "" {
			lines = append(lines, fmt.Sprintf("Author: %s", authorText))
		}
	}
	if details.durationLabel != "" {
		lines = append(lines, fmt.Sprintf("Duration: %s", details.durationLabel))
	}
	if details.statusSummary == "failed" && details.diagnosticURL != "" {
		lines = append(lines, fmt.Sprintf("Next: %s", slackMrkdwnLink(details.diagnosticURL, notificationDiagnosticLabel(details))))
	}
	if details.statusSummary == "succeeded" {
		artifactLine := formatNotificationArtifactSlackLine(details)
		if artifactLine != "" {
			lines = append(lines, artifactLine)
		}
	}
	lines = append(lines, notificationSlackCLIHintLines(details)...)
	if details.buildURL != "" && (details.statusSummary != "failed" || details.buildURL != details.diagnosticURL) {
		lines = append(lines, fmt.Sprintf("Build: %s", slackMrkdwnLink(details.buildURL, "View build")))
	}
	return strings.Join(lines, "\n")
}

func formatBuildStatusHeadline(details buildNotificationDetails) string {
	context := strings.TrimSpace(details.jobName)
	if context == "" {
		context = strings.TrimSpace(details.jobLabel)
	}
	if context == "" {
		context = notificationBuildContextLabel(details)
	}
	if context == "" {
		return fmt.Sprintf("%s Build %s", slackStatusIndicator(details.statusSummary), details.statusSummary)
	}
	return fmt.Sprintf("%s Build %s: %s", slackStatusIndicator(details.statusSummary), details.statusSummary, slackEscapeMrkdwnLabel(context))
}

func notificationBuildContextLabel(details buildNotificationDetails) string {
	if details.buildNumber > 0 {
		return fmt.Sprintf("#%d", details.buildNumber)
	}
	return strings.TrimSpace(details.buildID)
}

func notificationBuildLinkLabel(details buildNotificationDetails) string {
	if details.buildNumber > 0 {
		if strings.TrimSpace(details.buildID) == "" {
			return fmt.Sprintf("#%d", details.buildNumber)
		}
		return fmt.Sprintf("#%d (%s)", details.buildNumber, strings.TrimSpace(details.buildID))
	}
	return strings.TrimSpace(details.buildLabel)
}

func notificationSlackCommitText(details buildNotificationDetails) string {
	gitLabel := joinNotificationGitParts(slackGitRefLabel(details.refLabel), details.shaLabel)
	if gitLabel == "" {
		gitLabel = joinNotificationGitParts(details.refLabel, details.shaLabel)
	}
	if details.commitURL != "" {
		return slackMrkdwnLink(details.commitURL, gitLabel)
	}
	return slackEscapeMrkdwnLabel(gitLabel)
}

func notificationStepSlackText(step buildNotificationStep) string {
	if step.url != "" {
		return slackMrkdwnLink(step.url, step.label)
	}
	return slackEscapeMrkdwnLabel(step.label)
}

func notificationDiagnosticLabel(details buildNotificationDetails) string {
	if details.failedStep != nil {
		return "Open failed step logs"
	}
	return "View build details"
}

func notificationSlackCLIHintLines(details buildNotificationDetails) []string {
	buildID := strings.TrimSpace(details.buildID)
	if buildID == "" {
		return nil
	}

	statusCommand := notificationSlackInlineCode(fmt.Sprintf("coyote build status %s", buildID))
	switch details.statusSummary {
	case "failed":
		logsCommand := notificationSlackInlineCode(notificationSlackFailedLogsCommand(buildID, details.failedStep))
		retryCommand := notificationSlackInlineCode(fmt.Sprintf("coyote build retry %s --yes", buildID))
		return []string{"CLI:", statusCommand, logsCommand, retryCommand}
	case "succeeded":
		return []string{fmt.Sprintf("CLI: %s", statusCommand)}
	default:
		return nil
	}
}

func notificationSlackFailedLogsCommand(buildID string, failedStep *buildNotificationStep) string {
	if failedStep != nil {
		return fmt.Sprintf("coyote build logs %s --step %d --tail 200", buildID, failedStep.index)
	}
	return fmt.Sprintf("coyote build logs %s --failed --tail 200", buildID)
}

func notificationSlackInlineCode(command string) string {
	return fmt.Sprintf("`%s`", strings.TrimSpace(command))
}

func formatNotificationArtifactSlackLine(details buildNotificationDetails) string {
	if len(details.artifacts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(details.artifacts)+1)
	for _, artifact := range details.artifacts {
		parts = append(parts, slackMrkdwnLink(artifact.url, artifact.label))
	}
	overflow := details.artifactCount - len(details.artifacts)
	if overflow > 0 {
		if details.artifactsURL != "" {
			parts = append(parts, slackMrkdwnLink(details.artifactsURL, fmt.Sprintf("+%d more", overflow)))
		} else {
			parts = append(parts, fmt.Sprintf("+%d more", overflow))
		}
	}
	return "Artifacts: " + strings.Join(parts, ", ")
}

func buildNotificationFailedStep(publicBaseURL string, buildID string, steps []domain.BuildStep) *buildNotificationStep {
	if len(steps) == 0 {
		return nil
	}
	ordered := append([]domain.BuildStep(nil), steps...)
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].StepIndex < ordered[j].StepIndex
	})
	for _, step := range ordered {
		if step.Status != domain.BuildStepStatusFailed {
			continue
		}
		stepCopy := step
		return &buildNotificationStep{
			index: stepCopy.StepIndex,
			name:  strings.TrimSpace(stepCopy.Name),
			label: formatNotificationStepLabel(stepCopy),
			url:   buildBuildStepDetailURL(publicBaseURL, buildID, stepCopy.StepIndex),
		}
	}
	return nil
}

func stepErrorPointer(steps []domain.BuildStep, stepIndex int) *string {
	for i := range steps {
		if steps[i].StepIndex == stepIndex {
			return steps[i].ErrorMessage
		}
	}
	return nil
}

func stepExitCodePointer(steps []domain.BuildStep, stepIndex int) *int {
	for i := range steps {
		if steps[i].StepIndex == stepIndex {
			return steps[i].ExitCode
		}
	}
	return nil
}

func formatNotificationStepLabel(step domain.BuildStep) string {
	stepNumber := step.StepIndex + 1
	name := strings.TrimSpace(step.Name)
	if name == "" {
		return fmt.Sprintf("Step %d", stepNumber)
	}
	return fmt.Sprintf("Step %d %s", stepNumber, name)
}

func buildNotificationArtifactLinks(publicBaseURL string, artifacts []domain.BuildArtifact) []notificationArtifactLink {
	if publicBaseURL == "" || len(artifacts) == 0 {
		return nil
	}
	ordered := append([]domain.BuildArtifact(nil), artifacts...)
	sort.Slice(ordered, func(i, j int) bool {
		left := notificationArtifactSortKey(ordered[i])
		right := notificationArtifactSortKey(ordered[j])
		if left == right {
			return ordered[i].ID < ordered[j].ID
		}
		return left < right
	})
	links := make([]notificationArtifactLink, 0, minNotificationInt(len(ordered), maxNotificationArtifactLinks))
	for _, artifact := range ordered {
		artifactURL := buildArtifactDetailURL(publicBaseURL, artifact.ID)
		if artifactURL == "" {
			continue
		}
		links = append(links, notificationArtifactLink{
			label: truncateNotificationText(notificationArtifactDisplayLabel(artifact), maxNotificationArtifactLabelLength),
			url:   artifactURL,
		})
		if len(links) == maxNotificationArtifactLinks {
			break
		}
	}
	return links
}

func notificationArtifactDisplayLabel(artifact domain.BuildArtifact) string {
	baseLabel := strings.TrimSpace(artifact.Name)
	if baseLabel == "" {
		baseLabel = strings.TrimSpace(artifact.LogicalPath)
	}
	if baseLabel == "" {
		baseLabel = strings.TrimSpace(artifact.ID)
	}
	versionLabel := notificationArtifactVersionLabel(artifact.VersionTags)
	if versionLabel == "" {
		return baseLabel
	}
	return fmt.Sprintf("%s (%s)", baseLabel, versionLabel)
}

func notificationArtifactVersionLabel(tags []domain.VersionTag) string {
	for _, tag := range tags {
		if tag.Kind == domain.VersionTagKindVersion && strings.TrimSpace(tag.Version) != "" {
			return strings.TrimSpace(tag.Version)
		}
	}
	for _, tag := range tags {
		if strings.TrimSpace(tag.Version) != "" {
			return strings.TrimSpace(tag.Version)
		}
	}
	return ""
}

func notificationArtifactSortKey(artifact domain.BuildArtifact) string {
	if logicalPath := strings.TrimSpace(artifact.LogicalPath); logicalPath != "" {
		return logicalPath
	}
	if name := strings.TrimSpace(artifact.Name); name != "" {
		return name
	}
	return strings.TrimSpace(artifact.ID)
}

func truncateNotificationText(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if maxRunes <= 0 || trimmed == "" {
		return trimmed
	}
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	if maxRunes <= 1 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-1]) + "…"
}

func minNotificationInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
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

func buildBuildStepDetailURL(publicBaseURL string, buildID string, stepIndex int) string {
	buildURL := buildBuildDetailURL(publicBaseURL, buildID)
	if buildURL == "" || stepIndex < 0 {
		return buildURL
	}
	return buildURL + "?step=" + url.QueryEscape(strconv.Itoa(stepIndex))
}

func buildArtifactDetailURL(publicBaseURL string, artifactID string) string {
	return buildFrontendEntityURL(publicBaseURL, "/artifacts/", artifactID)
}

func buildArtifactsListURL(publicBaseURL string, buildID string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBaseURL), "/")
	trimmedID := strings.TrimSpace(buildID)
	if base == "" || trimmedID == "" {
		return ""
	}
	values := url.Values{}
	values.Set("build_id", trimmedID)
	return base + "/artifacts?" + values.Encode()
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
