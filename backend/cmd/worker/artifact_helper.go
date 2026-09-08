package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/artifact"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func runArtifactCollectAfterBuild(ctx context.Context) error {
	podName := strings.TrimSpace(os.Getenv(workspaceHelperPodName))
	namespace := strings.TrimSpace(os.Getenv(workspaceHelperNamespace))
	podUID := strings.TrimSpace(os.Getenv(workspaceHelperPodUID))
	if podName == "" || namespace == "" || podUID == "" {
		return errors.New("artifact collection requires pod name, namespace, and pod UID")
	}
	client, clientErr := newWorkspacePublishPodClient()
	if clientErr != nil {
		return fmt.Errorf("create Kubernetes Pod client: %w", clientErr)
	}
	buildSucceeded, waitErr := waitForSuccessfulBuild(ctx, client, podName, podUID)
	if waitErr != nil {
		return waitErr
	}
	return collectPodArtifacts(ctx, buildSucceeded)
}

func collectPodArtifacts(ctx context.Context, buildSucceeded bool) error {
	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv(workspaceHelperAPIURL)), "/")
	tokenPath := strings.TrimSpace(os.Getenv(workspaceHelperTokenPath))
	executionJobID := strings.TrimSpace(os.Getenv(workspaceHelperExecutionJobID))
	podUID := strings.TrimSpace(os.Getenv(workspaceHelperPodUID))
	workspacePath := strings.TrimSpace(os.Getenv(workspaceHelperWorkspacePath))
	if apiURL == "" || tokenPath == "" || executionJobID == "" || podUID == "" || workspacePath == "" {
		return errors.New("artifact collection requires internal API URL, token path, execution job ID, pod UID, and workspace path")
	}
	projectedToken, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		return fmt.Errorf("read artifact helper token: %w", readErr)
	}
	capability, exchangeErr := exchangeWorkspaceCapability(ctx, apiURL, strings.TrimSpace(string(projectedToken)), executionJobID, podUID, domain.WorkspaceHelperRoleArtifactCollect)
	if exchangeErr != nil {
		return exchangeErr
	}
	planRequest := fmt.Sprintf(`{"execution_job_id":%q,"pod_uid":%q,"build_succeeded":%t}`, executionJobID, podUID, buildSucceeded)
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/artifacts/plan", strings.NewReader(planRequest))
	if requestErr != nil {
		return requestErr
	}
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/json")
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		return doErr
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("artifact plan returned HTTP %d", response.StatusCode)
	}
	var payload struct {
		Data struct {
			Collect bool `json:"collect"`
			Scopes  []struct {
				StepID   string   `json:"step_id"`
				Patterns []string `json:"patterns"`
			} `json:"scopes"`
		} `json:"data"`
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(&payload); decodeErr != nil {
		return decodeErr
	}
	if !payload.Data.Collect {
		return nil
	}
	for _, scope := range payload.Data.Scopes {
		uploadErr := uploadArtifactScope(ctx, apiURL, capability, executionJobID, podUID, workspacePath, buildSucceeded, scope.StepID, scope.Patterns)
		if uploadErr != nil {
			return uploadErr
		}
	}
	return nil
}

func uploadArtifactScope(ctx context.Context, apiURL, capability, executionJobID, podUID, workspacePath string, buildSucceeded bool, stepID string, patterns []string) error {
	return filepath.WalkDir(workspacePath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			return infoErr
		}
		logicalPath, relativeErr := filepath.Rel(workspacePath, path)
		if relativeErr != nil {
			return relativeErr
		}
		logicalPath = filepath.ToSlash(logicalPath)
		matched := false
		for _, pattern := range patterns {
			if artifact.MatchPathPattern(pattern, logicalPath) {
				matched = true
				break
			}
		}
		if !matched {
			return nil
		}
		return func() error {
			file, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			defer func() { _ = file.Close() }()
			upload, newRequestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/artifacts/upload", file)
			if newRequestErr != nil {
				return newRequestErr
			}
			upload.Header.Set("Authorization", "Bearer "+capability)
			upload.Header.Set("Coyote-Execution-Job-ID", executionJobID)
			upload.Header.Set("Coyote-Pod-UID", podUID)
			upload.Header.Set("Coyote-Step-ID", stepID)
			upload.Header.Set("Coyote-Artifact-Path", logicalPath)
			upload.Header.Set("Coyote-Build-Succeeded", fmt.Sprintf("%t", buildSucceeded))
			uploadResponse, uploadDoErr := http.DefaultClient.Do(upload)
			if uploadDoErr != nil {
				return uploadDoErr
			}
			_, _ = io.Copy(io.Discard, uploadResponse.Body)
			_ = uploadResponse.Body.Close()
			if uploadResponse.StatusCode != http.StatusNoContent {
				return fmt.Errorf("artifact upload returned HTTP %d", uploadResponse.StatusCode)
			}
			return nil
		}()
	})
}
