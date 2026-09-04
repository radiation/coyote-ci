package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

const workspaceHelperWorkspacePath = "COYOTE_WORKSPACE_PATH"

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
