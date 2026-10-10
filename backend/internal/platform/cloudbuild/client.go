package cloudbuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	artifactregistry "google.golang.org/api/artifactregistry/v1"
	googlecloudbuild "google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

var validLogicalImageName = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]*$`)

type Config struct {
	ProjectID                  string
	Location                   string
	RuntimeServiceAccount      string
	ArtifactRegistryRepository string
}

type Client struct {
	projectID                  string
	location                   string
	runtimeServiceAccount      string
	artifactRegistryRepository string
	builds                     *googlecloudbuild.ProjectsLocationsBuildsService
	registry                   *artifactregistry.Service
	registryRepositoryName     string
}

func New(ctx context.Context, config Config, options ...option.ClientOption) (*Client, error) {
	if strings.TrimSpace(config.ProjectID) == "" || strings.TrimSpace(config.Location) == "" || strings.TrimSpace(config.RuntimeServiceAccount) == "" || strings.TrimSpace(config.ArtifactRegistryRepository) == "" {
		return nil, errors.New("cloud build project, location, runtime service account, and artifact registry repository are required")
	}
	service, err := googlecloudbuild.NewService(ctx, options...)
	if err != nil {
		return nil, err
	}
	registry, err := artifactregistry.NewService(ctx, options...)
	if err != nil {
		return nil, err
	}
	repository := strings.TrimSuffix(strings.TrimSpace(config.ArtifactRegistryRepository), "/")
	repositoryName, repositoryErr := artifactRegistryRepositoryName(repository)
	if repositoryErr != nil {
		return nil, repositoryErr
	}
	return &Client{projectID: strings.TrimSpace(config.ProjectID), location: strings.TrimSpace(config.Location), runtimeServiceAccount: strings.TrimSpace(config.RuntimeServiceAccount), artifactRegistryRepository: repository, builds: googlecloudbuild.NewProjectsLocationsBuildsService(service), registry: registry, registryRepositoryName: repositoryName}, nil
}

func (c *Client) ResolveTargetImageReference(logicalName, applicationVersion string) (string, error) {
	targetImage, err := c.targetImageReference(logicalName)
	if err != nil || strings.TrimSpace(applicationVersion) == "" {
		return targetImage, err
	}
	return targetImage + ":" + applicationVersion, nil
}

func (c *Client) Submit(ctx context.Context, request domain.ImageBuildRequest) (domain.ImageBuildHandle, error) {
	build, buildErr := c.buildRequest(request)
	if buildErr != nil {
		return domain.ImageBuildHandle{}, buildErr
	}
	operation, err := c.builds.Create(c.parent(), build).Context(ctx).Do()
	if err != nil {
		return domain.ImageBuildHandle{}, &submissionError{err: err, retryable: isRetryableAPIError(err)}
	}
	var metadata googlecloudbuild.BuildOperationMetadata
	if err := json.Unmarshal(operation.Metadata, &metadata); err != nil || metadata.Build == nil || strings.TrimSpace(metadata.Build.Id) == "" {
		return domain.ImageBuildHandle{}, fmt.Errorf("cloud build create response missing build identity: %w", err)
	}
	return domain.ImageBuildHandle{ID: metadata.Build.Id, ResourceName: metadata.Build.Name}, nil
}

func (c *Client) Get(ctx context.Context, handle domain.ImageBuildHandle) (domain.ImageBuildResult, error) {
	build, err := c.builds.Get(c.resourceName(handle)).Context(ctx).Do()
	if err != nil {
		return domain.ImageBuildResult{}, err
	}
	return resultFromBuild(build), nil
}

func (c *Client) Cancel(ctx context.Context, handle domain.ImageBuildHandle) error {
	_, err := c.builds.Cancel(c.resourceName(handle), &googlecloudbuild.CancelBuildRequest{}).Context(ctx).Do()
	return err
}

func (c *Client) FindByExecutionJobID(ctx context.Context, executionJobID string) (domain.ImageBuildHandle, bool, error) {
	response, err := c.builds.List(c.parent()).Filter(fmt.Sprintf("tags = coyote-execution-%s", executionJobID)).Context(ctx).Do()
	if err != nil {
		return domain.ImageBuildHandle{}, false, err
	}
	if len(response.Builds) == 0 {
		return domain.ImageBuildHandle{}, false, nil
	}
	build := response.Builds[0]
	return domain.ImageBuildHandle{ID: build.Id, ResourceName: build.Name}, true, nil
}

func (c *Client) buildRequest(request domain.ImageBuildRequest) (*googlecloudbuild.Build, error) {
	generation, generationErr := strconv.ParseInt(request.Source.Generation, 10, 64)
	if generationErr != nil || generation <= 0 {
		return nil, fmt.Errorf("invalid source generation %q", request.Source.Generation)
	}
	targetImage := strings.TrimSpace(request.PublishedImageReference)
	if targetImage == "" {
		var targetErr error
		targetImage, targetErr = c.ResolveTargetImageReference(request.Spec.TargetImageReference, "")
		if targetErr != nil {
			return nil, targetErr
		}
	}
	args := []string{"build", "--file=" + request.Spec.DockerfilePath, "--tag=" + targetImage}
	if target := strings.TrimSpace(request.Spec.Target); target != "" {
		args = append(args, "--target="+target)
	}
	for key, value := range request.Spec.BuildArgs {
		args = append(args, "--build-arg="+key+"="+value)
	}
	args = append(args, request.Spec.ContextPath)
	return &googlecloudbuild.Build{
		Source:         &googlecloudbuild.Source{StorageSource: &googlecloudbuild.StorageSource{Bucket: request.Source.Bucket, Object: request.Source.Object, Generation: generation}},
		Steps:          []*googlecloudbuild.BuildStep{{Name: "gcr.io/cloud-builders/docker", Args: args}},
		Images:         []string{targetImage},
		Options:        &googlecloudbuild.BuildOptions{Logging: "CLOUD_LOGGING_ONLY"},
		Timeout:        protobufDurationSeconds(request.Timeout),
		ServiceAccount: c.serviceAccountResourceName(),
		Tags:           []string{"coyote-execution-" + request.ExecutionJobID},
	}, nil
}

func protobufDurationSeconds(timeout time.Duration) string {
	return strconv.FormatInt(int64(timeout/time.Second), 10) + "s"
}

type submissionError struct {
	err       error
	retryable bool
}

func (e *submissionError) Error() string   { return e.err.Error() }
func (e *submissionError) Unwrap() error   { return e.err }
func (e *submissionError) Retryable() bool { return e.retryable }

func isRetryableAPIError(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == http.StatusRequestTimeout || apiErr.Code == http.StatusTooManyRequests || apiErr.Code >= http.StatusInternalServerError
	}
	var networkErr net.Error
	return errors.As(err, &networkErr) && networkErr.Timeout()
}

func (c *Client) targetImageReference(logicalName string) (string, error) {
	trimmed := strings.TrimSpace(logicalName)
	if !validLogicalImageName.MatchString(trimmed) || path.Clean(trimmed) != trimmed || strings.HasPrefix(trimmed, "/") {
		return "", fmt.Errorf("invalid logical image name %q", logicalName)
	}
	return c.artifactRegistryRepository + "/" + trimmed, nil
}

func (c *Client) EnsureImmutableTags(ctx context.Context, publishedImageReference string) error {
	if !c.ownsPublishedImageReference(publishedImageReference) {
		return fmt.Errorf("%w: image %q is outside configured Artifact Registry repository", service.ErrImmutableImageTagsRequired, publishedImageReference)
	}
	repository, err := c.registry.Projects.Locations.Repositories.Get(c.registryRepositoryName).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("checking Artifact Registry immutable tag configuration: %w", err)
	}
	if repository.DockerConfig == nil || !repository.DockerConfig.ImmutableTags {
		return fmt.Errorf("%w: Artifact Registry repository %q does not enable immutable tags", service.ErrImmutableImageTagsRequired, c.artifactRegistryRepository)
	}
	return nil
}

func (c *Client) ResolvePublishedDigest(ctx context.Context, publishedImageReference string) (string, bool, error) {
	if !c.ownsPublishedImageReference(publishedImageReference) {
		return "", false, fmt.Errorf("image %q is outside configured Artifact Registry repository", publishedImageReference)
	}
	tag, ok := imageTag(publishedImageReference)
	if !ok {
		return "", false, fmt.Errorf("image %q does not include a tag", publishedImageReference)
	}
	tagName := c.registryRepositoryName + "/packages/" + url.PathEscape(strings.TrimPrefix(imageRepository(publishedImageReference), c.artifactRegistryRepository+"/")) + "/tags/" + url.PathEscape(tag)
	resolvedTag, err := c.registry.Projects.Locations.Repositories.Packages.Tags.Get(tagName).Context(ctx).Do()
	if isNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("resolving Artifact Registry image tag %q: %w", publishedImageReference, err)
	}
	versionMarker := "/versions/"
	versionIndex := strings.LastIndex(resolvedTag.Version, versionMarker)
	if versionIndex == -1 {
		return "", false, fmt.Errorf("artifact Registry tag %q returned an invalid version reference %q", publishedImageReference, resolvedTag.Version)
	}
	digest := resolvedTag.Version[versionIndex+len(versionMarker):]
	if !strings.HasPrefix(digest, "sha256:") {
		return "", false, fmt.Errorf("artifact Registry tag %q returned a non-digest version reference %q", publishedImageReference, resolvedTag.Version)
	}
	return imageRepository(publishedImageReference) + "@" + digest, true, nil
}

func isNotFound(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == http.StatusNotFound
}

func (c *Client) ownsPublishedImageReference(reference string) bool {
	return strings.HasPrefix(strings.TrimSpace(reference), c.artifactRegistryRepository+"/")
}

func artifactRegistryRepositoryName(repository string) (string, error) {
	parts := strings.Split(strings.TrimSpace(repository), "/")
	if len(parts) != 3 || !strings.HasSuffix(parts[0], "-docker.pkg.dev") || strings.TrimSpace(parts[1]) == "" || strings.TrimSpace(parts[2]) == "" {
		return "", fmt.Errorf("invalid Artifact Registry repository %q", repository)
	}
	location := strings.TrimSuffix(parts[0], "-docker.pkg.dev")
	if location == "" {
		return "", fmt.Errorf("invalid Artifact Registry repository %q", repository)
	}
	return fmt.Sprintf("projects/%s/locations/%s/repositories/%s", parts[1], location, parts[2]), nil
}

func imageTag(reference string) (string, bool) {
	lastSlash := strings.LastIndex(reference, "/")
	lastColon := strings.LastIndex(reference, ":")
	if lastColon <= lastSlash || lastColon == len(reference)-1 {
		return "", false
	}
	return reference[lastColon+1:], true
}

func imageRepository(reference string) string {
	lastSlash := strings.LastIndex(reference, "/")
	lastColon := strings.LastIndex(reference, ":")
	if lastColon > lastSlash {
		return reference[:lastColon]
	}
	return reference
}

func (c *Client) serviceAccountResourceName() string {
	return fmt.Sprintf("projects/%s/serviceAccounts/%s", c.projectID, c.runtimeServiceAccount)
}

func (c *Client) parent() string {
	return fmt.Sprintf("projects/%s/locations/%s", c.projectID, c.location)
}
func (c *Client) resourceName(handle domain.ImageBuildHandle) string {
	if strings.TrimSpace(handle.ResourceName) != "" {
		return handle.ResourceName
	}
	return c.parent() + "/builds/" + handle.ID
}

func resultFromBuild(build *googlecloudbuild.Build) domain.ImageBuildResult {
	result := domain.ImageBuildResult{Status: mapStatus(build.Status), ExternalLogURL: build.LogUrl, FailureDetail: build.StatusDetail}
	result.Timing = cloudBuildTiming(build.CreateTime, build.StartTime, build.FinishTime)
	if build.FailureInfo != nil && build.FailureInfo.Detail != "" {
		result.FailureDetail = build.FailureInfo.Detail
	}
	if build.Results != nil && len(build.Results.Images) > 0 {
		image := build.Results.Images[0]
		result.PublishedImageReference = image.Name
		result.ImageDigest = image.Digest
		if image.Name != "" && strings.HasPrefix(image.Digest, "sha256:") {
			result.PublishedImageDigestReference = imageRepository(image.Name) + "@" + image.Digest
		}
	}
	return result
}

func cloudBuildTiming(createdAt, startedAt, finishedAt string) *domain.ExecutionTiming {
	parse := func(value string) *time.Time {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil
		}
		return &parsed
	}
	submitted := parse(createdAt)
	started := parse(startedAt)
	finished := parse(finishedAt)
	if submitted == nil && started == nil && finished == nil {
		return nil
	}
	phases := []domain.ExecutionPhaseTiming{{Name: "submission", StartedAt: submitted}}
	if submitted != nil {
		phases = append(phases, domain.ExecutionPhaseTiming{Name: "queue", StartedAt: submitted, FinishedAt: started})
	}
	if started != nil || finished != nil {
		phases = append(phases, domain.ExecutionPhaseTiming{Name: "command", StartedAt: started, FinishedAt: finished})
	}
	if submitted != nil && finished != nil {
		phases = append(phases, domain.ExecutionPhaseTiming{Name: "total_execution", StartedAt: submitted, FinishedAt: finished})
	}
	return &domain.ExecutionTiming{Phases: phases}
}

func mapStatus(status string) domain.ImageBuildStatus {
	switch status {
	case "PENDING", "QUEUED":
		return domain.ImageBuildStatusQueued
	case "WORKING":
		return domain.ImageBuildStatusRunning
	case "SUCCESS":
		return domain.ImageBuildStatusSuccess
	case "CANCELLED":
		return domain.ImageBuildStatusCanceled
	default:
		return domain.ImageBuildStatusFailed
	}
}
