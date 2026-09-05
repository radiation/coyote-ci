package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const workspaceHelperWorkspacePath = "COYOTE_WORKSPACE_PATH"

const (
	workspaceHelperPodName   = "COYOTE_WORKSPACE_HELPER_POD_NAME"
	workspaceHelperNamespace = "COYOTE_WORKSPACE_HELPER_NAMESPACE"
)

type workspacePublishPodClient interface {
	Get(context.Context, string, metav1.GetOptions) (*corev1.Pod, error)
}

var newWorkspacePublishPodClient = func() (workspacePublishPodClient, error) {
	config, configErr := rest.InClusterConfig()
	if configErr != nil {
		return nil, configErr
	}
	client, clientErr := kubernetes.NewForConfig(config)
	if clientErr != nil {
		return nil, clientErr
	}
	return client.CoreV1().Pods(strings.TrimSpace(os.Getenv(workspaceHelperNamespace))), nil
}

func runWorkspacePublish(ctx context.Context) error {
	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv(workspaceHelperAPIURL)), "/")
	tokenPath := strings.TrimSpace(os.Getenv(workspaceHelperTokenPath))
	executionJobID := strings.TrimSpace(os.Getenv(workspaceHelperExecutionJobID))
	podUID := strings.TrimSpace(os.Getenv(workspaceHelperPodUID))
	workspacePath := strings.TrimSpace(os.Getenv(workspaceHelperWorkspacePath))
	if apiURL == "" || tokenPath == "" || executionJobID == "" || podUID == "" || workspacePath == "" {
		return errors.New("workspace publish requires internal API URL, token path, execution job ID, pod UID, and workspace path")
	}
	projectedTokenBytes, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		return fmt.Errorf("read workspace helper token: %w", readErr)
	}
	capability, exchangeErr := exchangeWorkspaceCapability(ctx, apiURL, strings.TrimSpace(string(projectedTokenBytes)), executionJobID, podUID, domain.WorkspaceHelperRolePublish)
	if exchangeErr != nil {
		return exchangeErr
	}
	archive, publication, archiveErr := workspacepkg.ArchiveDirectory(ctx, workspacePath)
	if archiveErr != nil {
		return archiveErr
	}
	defer func() { _ = archive.Close() }()
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/publish", archive)
	if requestErr != nil {
		return requestErr
	}
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/gzip")
	request.Header.Set("Coyote-Execution-Job-ID", executionJobID)
	request.Header.Set("Coyote-Pod-UID", podUID)
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		return doErr
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("workspace publish returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data api.WorkspaceHelperPublishResponse `json:"data"`
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(&payload); decodeErr != nil {
		return decodeErr
	}
	if strings.TrimSpace(payload.Data.RevisionID) == "" || strings.TrimSpace(payload.Data.ContentDigest) == "" || payload.Data.SizeBytes < 0 {
		return errors.New("workspace publish returned an invalid publication result")
	}
	if payload.Data.RevisionID != domain.WorkspaceRevisionIDForExecutionJob(executionJobID) {
		return errors.New("workspace publish response has an unexpected revision ID")
	}
	if payload.Data.ContentDigest != publication.ContentDigest || publication.SizeBytes == nil || payload.Data.SizeBytes != *publication.SizeBytes {
		return errors.New("workspace publish response does not match local archive")
	}
	return nil
}

func runWorkspacePublishAfterBuild(ctx context.Context) error {
	podName := strings.TrimSpace(os.Getenv(workspaceHelperPodName))
	namespace := strings.TrimSpace(os.Getenv(workspaceHelperNamespace))
	podUID := strings.TrimSpace(os.Getenv(workspaceHelperPodUID))
	if podName == "" || namespace == "" || podUID == "" {
		return errors.New("workspace publish after build requires pod name, namespace, and pod UID")
	}
	client, clientErr := newWorkspacePublishPodClient()
	if clientErr != nil {
		return fmt.Errorf("create Kubernetes Pod client: %w", clientErr)
	}
	buildSucceeded, waitErr := waitForSuccessfulBuild(ctx, client, podName, podUID)
	if waitErr != nil {
		return waitErr
	}
	if !buildSucceeded {
		return nil
	}
	return runWorkspacePublish(ctx)
}

func waitForSuccessfulBuild(ctx context.Context, client workspacePublishPodClient, podName string, podUID string) (bool, error) {
	if client == nil {
		return false, errors.New("workspace publish requires a Kubernetes Pod client")
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		pod, getErr := client.Get(ctx, strings.TrimSpace(podName), metav1.GetOptions{})
		if getErr != nil {
			return false, fmt.Errorf("get workspace Pod status: %w", getErr)
		}
		terminal, succeeded, statusErr := buildContainerStatus(pod, podUID)
		if statusErr != nil {
			return false, statusErr
		}
		if terminal {
			return succeeded, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-ticker.C:
		}
	}
}

func buildContainerStatus(pod *corev1.Pod, expectedUID string) (bool, bool, error) {
	if pod == nil || string(pod.UID) != strings.TrimSpace(expectedUID) {
		return false, false, errors.New("workspace publish Pod UID does not match helper identity")
	}
	buildDeclared := false
	for _, container := range pod.Spec.Containers {
		if container.Name == "build" {
			buildDeclared = true
			break
		}
	}
	if !buildDeclared {
		return false, false, errors.New("workspace publish Pod has no build container")
	}
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == "build" && status.State.Terminated != nil {
			return true, status.State.Terminated.ExitCode == 0, nil
		}
	}
	return false, false, nil
}
