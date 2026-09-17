package cloudbuild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	googlecloudbuild "google.golang.org/api/cloudbuild/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"

	"github.com/radiation/coyote-ci/backend/internal/domain"
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
}

func New(ctx context.Context, config Config, options ...option.ClientOption) (*Client, error) {
	if strings.TrimSpace(config.ProjectID) == "" || strings.TrimSpace(config.Location) == "" || strings.TrimSpace(config.RuntimeServiceAccount) == "" || strings.TrimSpace(config.ArtifactRegistryRepository) == "" {
		return nil, errors.New("cloud build project, location, runtime service account, and artifact registry repository are required")
	}
	service, err := googlecloudbuild.NewService(ctx, options...)
	if err != nil {
		return nil, err
	}
	return &Client{projectID: strings.TrimSpace(config.ProjectID), location: strings.TrimSpace(config.Location), runtimeServiceAccount: strings.TrimSpace(config.RuntimeServiceAccount), artifactRegistryRepository: strings.TrimSuffix(strings.TrimSpace(config.ArtifactRegistryRepository), "/"), builds: googlecloudbuild.NewProjectsLocationsBuildsService(service)}, nil
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
	targetImage, targetErr := c.targetImageReference(request.Spec.TargetImageReference)
	if targetErr != nil {
		return nil, targetErr
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
		result.ImageDigest = build.Results.Images[0].Digest
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
