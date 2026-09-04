package main

import (
	"bytes"
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

const (
	workspaceHelperAPIURL         = "COYOTE_INTERNAL_API_URL"
	workspaceHelperTokenPath      = "COYOTE_WORKSPACE_HELPER_TOKEN_PATH"
	workspaceHelperExecutionJobID = "COYOTE_WORKSPACE_HELPER_EXECUTION_JOB_ID"
	workspaceHelperPodUID         = "COYOTE_WORKSPACE_HELPER_POD_UID"
	workspaceHelperDestination    = "COYOTE_WORKSPACE_DESTINATION"
)

func runWorkspacePrepare(ctx context.Context) error {
	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv(workspaceHelperAPIURL)), "/")
	tokenPath := strings.TrimSpace(os.Getenv(workspaceHelperTokenPath))
	executionJobID := strings.TrimSpace(os.Getenv(workspaceHelperExecutionJobID))
	podUID := strings.TrimSpace(os.Getenv(workspaceHelperPodUID))
	destination := strings.TrimSpace(os.Getenv(workspaceHelperDestination))
	if apiURL == "" || tokenPath == "" || executionJobID == "" || podUID == "" || destination == "" {
		return errors.New("workspace prepare requires internal API URL, token path, execution job ID, pod UID, and destination")
	}
	projectedTokenBytes, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		return fmt.Errorf("read workspace helper token: %w", readErr)
	}
	projectedToken := strings.TrimSpace(string(projectedTokenBytes))
	if projectedToken == "" {
		return errors.New("workspace helper token is empty")
	}
	capability, exchangeErr := exchangeWorkspacePrepareCapability(ctx, apiURL, projectedToken, executionJobID, podUID)
	if exchangeErr != nil {
		return exchangeErr
	}
	return downloadAndRestoreWorkspace(ctx, apiURL, capability, executionJobID, podUID, destination)
}

func exchangeWorkspacePrepareCapability(ctx context.Context, apiURL string, projectedToken string, executionJobID string, podUID string) (string, error) {
	return exchangeWorkspaceCapability(ctx, apiURL, projectedToken, executionJobID, podUID, domain.WorkspaceHelperRolePrepare)
}

func exchangeWorkspaceCapability(ctx context.Context, apiURL string, projectedToken string, executionJobID string, podUID string, role domain.WorkspaceHelperRole) (string, error) {
	body, marshalErr := json.Marshal(api.WorkspaceHelperCapabilityExchangeRequest{ExecutionJobID: executionJobID, PodUID: podUID, Role: string(role)})
	if marshalErr != nil {
		return "", marshalErr
	}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/capabilities", bytes.NewReader(body))
	if requestErr != nil {
		return "", requestErr
	}
	request.Header.Set("Authorization", "Bearer "+projectedToken)
	request.Header.Set("Content-Type", "application/json")
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		return "", doErr
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("workspace capability exchange returned HTTP %d", response.StatusCode)
	}
	var responsePayload struct {
		Data api.WorkspaceHelperCapabilityExchangeResponse `json:"data"`
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(&responsePayload); decodeErr != nil {
		return "", decodeErr
	}
	if strings.TrimSpace(responsePayload.Data.Capability) == "" {
		return "", errors.New("workspace capability exchange returned no capability")
	}
	return responsePayload.Data.Capability, nil
}

func downloadAndRestoreWorkspace(ctx context.Context, apiURL string, capability string, executionJobID string, podUID string, destination string) error {
	body, marshalErr := json.Marshal(api.WorkspaceHelperPrepareRequest{ExecutionJobID: executionJobID, PodUID: podUID})
	if marshalErr != nil {
		return marshalErr
	}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/prepare", bytes.NewReader(body))
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
		return fmt.Errorf("workspace prepare returned HTTP %d", response.StatusCode)
	}
	digest := strings.TrimSpace(response.Header.Get("Content-Digest"))
	if response.ContentLength < 0 || digest == "" {
		return errors.New("workspace prepare response lacks integrity metadata")
	}
	size := response.ContentLength
	publication := domain.WorkspaceRevisionPublication{StorageKey: "workspace-revisions/transport.tar.gz", ContentDigest: digest, SizeBytes: &size}
	return workspacepkg.RestoreArchive(ctx, response.Body, publication, destination)
}
