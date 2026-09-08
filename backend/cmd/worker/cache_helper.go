package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/radiation/coyote-ci/backend/internal/api"
	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

const (
	cacheHelperRoot       = "COYOTE_CACHE_ROOT"
	cacheHelperPreset     = "COYOTE_CACHE_PRESET"
	cacheHelperPolicy     = "COYOTE_CACHE_POLICY"
	cacheHelperWorkingDir = "COYOTE_CACHE_WORKING_DIR"
	cacheHelperStateRoot  = "COYOTE_CACHE_STATE_ROOT"
)

func runCacheRestore(ctx context.Context) error {
	apiURL, tokenPath, executionJobID, podUID, root, preset, stateRoot, policy, err := cacheHelperConfig()
	if err != nil {
		return err
	}
	if ensureErr := ensureCacheMountPaths(root, preset, strings.TrimSpace(os.Getenv(cacheHelperWorkingDir))); ensureErr != nil {
		return ensureErr
	}
	if policy == domain.CachePolicyOff {
		return nil
	}
	key, keyErr := cacheKeyForHelper(root, preset)
	if keyErr != nil {
		return reportCacheSideEffectError("restore", keyErr)
	}
	if saveErr := savePreparedCacheKey(stateRoot, key); saveErr != nil {
		return saveErr
	}
	if policy == domain.CachePolicyPush {
		return nil
	}
	projectedToken, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		return reportCacheSideEffectError("restore", fmt.Errorf("read cache helper token: %w", readErr))
	}
	capability, exchangeErr := exchangeWorkspaceCapability(ctx, apiURL, strings.TrimSpace(string(projectedToken)), executionJobID, podUID, domain.WorkspaceHelperRoleCacheRestore)
	if exchangeErr != nil {
		return reportCacheSideEffectError("restore", exchangeErr)
	}
	body, marshalErr := json.Marshal(api.WorkspaceHelperCacheRequest{ExecutionJobID: executionJobID, PodUID: podUID, Preset: preset, CacheKey: key})
	if marshalErr != nil {
		return reportCacheSideEffectError("restore", marshalErr)
	}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/cache/restore", bytes.NewReader(body))
	if requestErr != nil {
		return reportCacheSideEffectError("restore", requestErr)
	}
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/json")
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		return reportCacheSideEffectError("restore", doErr)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		return nil
	}
	if response.StatusCode != http.StatusOK {
		return reportCacheSideEffectError("restore", fmt.Errorf("cache restore returned HTTP %d", response.StatusCode))
	}
	size := response.ContentLength
	publication := domain.WorkspaceRevisionPublication{StorageKey: "cache/transport.tar.gz", ContentDigest: strings.TrimSpace(response.Header.Get("Content-Digest")), SizeBytes: &size}
	return reportCacheSideEffectError("restore", restoreCacheArchive(ctx, response.Body, publication, root, preset, strings.TrimSpace(os.Getenv(cacheHelperWorkingDir))))
}

func restoreCacheArchive(ctx context.Context, archive io.Reader, publication domain.WorkspaceRevisionPublication, root string, preset string, workingDir string) error {
	if removeErr := os.RemoveAll(root); removeErr != nil {
		return removeErr
	}
	restoreErr := workspacepkg.RestoreArchive(ctx, archive, publication, root)
	mountErr := ensureCacheMountPaths(root, preset, workingDir)
	if restoreErr != nil {
		return restoreErr
	}
	return mountErr
}

func runCacheSaveAfterBuild(ctx context.Context) error {
	podName := strings.TrimSpace(os.Getenv(workspaceHelperPodName))
	namespace := strings.TrimSpace(os.Getenv(workspaceHelperNamespace))
	podUID := strings.TrimSpace(os.Getenv(workspaceHelperPodUID))
	if podName == "" || namespace == "" || podUID == "" {
		return reportCacheSideEffectError("save", errors.New("cache save after build requires pod name, namespace, and pod UID"))
	}
	client, clientErr := newWorkspacePublishPodClient()
	if clientErr != nil {
		return reportCacheSideEffectError("save", fmt.Errorf("create Kubernetes Pod client: %w", clientErr))
	}
	succeeded, waitErr := waitForSuccessfulBuild(ctx, client, podName, podUID)
	if waitErr != nil || !succeeded {
		return reportCacheSideEffectError("save", waitErr)
	}
	return runCacheSave(ctx)
}

func runCacheSave(ctx context.Context) error {
	apiURL, tokenPath, executionJobID, podUID, root, preset, stateRoot, policy, err := cacheHelperConfig()
	if err != nil || policy == domain.CachePolicyOff || policy == domain.CachePolicyPull {
		return err
	}
	key, keyErr := loadPreparedCacheKey(stateRoot)
	if keyErr != nil {
		return reportCacheSideEffectError("save", keyErr)
	}
	projectedToken, readErr := os.ReadFile(tokenPath)
	if readErr != nil {
		return reportCacheSideEffectError("save", fmt.Errorf("read cache helper token: %w", readErr))
	}
	capability, exchangeErr := exchangeWorkspaceCapability(ctx, apiURL, strings.TrimSpace(string(projectedToken)), executionJobID, podUID, domain.WorkspaceHelperRoleCacheSave)
	if exchangeErr != nil {
		return reportCacheSideEffectError("save", exchangeErr)
	}
	archive, publication, archiveErr := workspacepkg.ArchiveDirectory(ctx, root)
	if archiveErr != nil {
		return reportCacheSideEffectError("save", archiveErr)
	}
	defer func() { _ = archive.Close() }()
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/cache/save", archive)
	if requestErr != nil {
		return reportCacheSideEffectError("save", requestErr)
	}
	request.ContentLength = *publication.SizeBytes
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/gzip")
	request.Header.Set("Content-Digest", publication.ContentDigest)
	request.Header.Set("Content-Length", fmt.Sprintf("%d", *publication.SizeBytes))
	request.Header.Set("Coyote-Execution-Job-ID", executionJobID)
	request.Header.Set("Coyote-Pod-UID", podUID)
	request.Header.Set("Coyote-Cache-Preset", preset)
	request.Header.Set("Coyote-Cache-Key", key)
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		return reportCacheSideEffectError("save", doErr)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		return reportCacheSideEffectError("save", fmt.Errorf("cache save returned HTTP %d", response.StatusCode))
	}
	return nil
}

func reportCacheSideEffectError(operation string, err error) error {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cache %s failed: %v\n", operation, err)
	}
	return nil
}

func cacheHelperConfig() (string, string, string, string, string, string, string, domain.CachePolicy, error) {
	apiURL := strings.TrimRight(strings.TrimSpace(os.Getenv(workspaceHelperAPIURL)), "/")
	tokenPath := strings.TrimSpace(os.Getenv(workspaceHelperTokenPath))
	executionJobID := strings.TrimSpace(os.Getenv(workspaceHelperExecutionJobID))
	podUID := strings.TrimSpace(os.Getenv(workspaceHelperPodUID))
	root := strings.TrimSpace(os.Getenv(cacheHelperRoot))
	preset := strings.TrimSpace(os.Getenv(cacheHelperPreset))
	stateRoot := strings.TrimSpace(os.Getenv(cacheHelperStateRoot))
	policy := domain.NormalizeCachePolicy(domain.CachePolicy(os.Getenv(cacheHelperPolicy)))
	if apiURL == "" || tokenPath == "" || executionJobID == "" || podUID == "" || root == "" || preset == "" || stateRoot == "" {
		return "", "", "", "", "", "", "", policy, errors.New("cache helper requires internal API URL, token path, execution job ID, pod UID, root, preset, and state root")
	}
	return apiURL, tokenPath, executionJobID, podUID, root, preset, stateRoot, policy, nil
}

func savePreparedCacheKey(stateRoot string, key string) error {
	if mkdirErr := os.MkdirAll(stateRoot, 0o755); mkdirErr != nil {
		return fmt.Errorf("create cache helper state directory: %w", mkdirErr)
	}
	if writeErr := os.WriteFile(filepath.Join(stateRoot, "prepared-key"), []byte(key), 0o600); writeErr != nil {
		return fmt.Errorf("write prepared cache key: %w", writeErr)
	}
	return nil
}

func loadPreparedCacheKey(stateRoot string) (string, error) {
	contents, readErr := os.ReadFile(filepath.Join(stateRoot, "prepared-key"))
	if readErr != nil {
		return "", fmt.Errorf("read prepared cache key: %w", readErr)
	}
	key := strings.TrimSpace(string(contents))
	if key == "" {
		return "", errors.New("prepared cache key is empty")
	}
	return key, nil
}

func cacheKeyForHelper(root string, preset string) (string, error) {
	workspaceRoot := strings.TrimSpace(os.Getenv(workspaceHelperWorkspacePath))
	workingDir := strings.TrimSpace(os.Getenv(cacheHelperWorkingDir))
	if workspaceRoot == "" {
		return "", errors.New("cache helper requires workspace path")
	}
	resolved, resolveErr := cachepkg.ResolvePreset(preset, workingDir)
	if resolveErr != nil {
		return "", resolveErr
	}
	fingerprint, _, fingerprintErr := cachepkg.ComputeFingerprint(workspaceRoot, resolved.FingerprintFiles)
	if fingerprintErr != nil {
		return "", fingerprintErr
	}
	return resolved.Name + ":" + fingerprint, nil
}

func ensureCacheMountPaths(root string, preset string, workingDir string) error {
	resolved, err := cachepkg.ResolvePreset(preset, workingDir)
	if err != nil {
		return err
	}
	for index := range resolved.CachePaths {
		if mkdirErr := os.MkdirAll(filepath.Join(root, "paths", fmt.Sprintf("%03d", index)), 0o755); mkdirErr != nil {
			return fmt.Errorf("create cache mount path: %w", mkdirErr)
		}
	}
	return nil
}
