package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/radiation/coyote-ci/backend/internal/api"
	cachepkg "github.com/radiation/coyote-ci/backend/internal/cache"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	workspacepkg "github.com/radiation/coyote-ci/backend/internal/workspace"
)

const (
	cacheHelperRoot              = "COYOTE_CACHE_ROOT"
	cacheHelperPreset            = "COYOTE_CACHE_PRESET"
	cacheHelperPolicy            = "COYOTE_CACHE_POLICY"
	cacheHelperComponentPolicies = "COYOTE_CACHE_COMPONENT_POLICIES"
	cacheHelperWorkingDir        = "COYOTE_CACHE_WORKING_DIR"
	cacheHelperStateRoot         = "COYOTE_CACHE_STATE_ROOT"
	cacheHelperComponents        = "COYOTE_CACHE_COMPONENTS"
	cacheHelperBuildImage        = "COYOTE_CACHE_BUILD_IMAGE"
)

const maxCacheHelperErrorResponseBytes = 4 * 1024

var archiveCacheDirectory = workspacepkg.ArchiveDirectory

func runCacheRestore(ctx context.Context) error {
	config, err := cacheHelperConfig()
	if err != nil {
		return err
	}
	if ensureErr := ensureCacheMountPaths(config.root, config.preset, strings.TrimSpace(os.Getenv(cacheHelperWorkingDir))); ensureErr != nil {
		return ensureErr
	}
	components, componentErr := cacheComponentsForHelper(config.preset, strings.TrimSpace(os.Getenv(cacheHelperWorkingDir)))
	if componentErr != nil {
		return reportCacheSideEffectError("restore", componentErr)
	}
	canRestore := false
	canSave := false
	for _, component := range components {
		policy := config.componentPolicy(component.Name)
		canRestore = canRestore || cachePolicyAllowsRestore(policy)
		canSave = canSave || cachePolicyAllowsSave(policy)
	}
	if !canRestore && !canSave {
		return nil
	}
	keys, keyErr := cacheKeysForHelper(components)
	if keyErr != nil {
		return reportCacheSideEffectError("restore", keyErr)
	}
	if saveErr := savePreparedCacheKeys(config.stateRoot, keys); saveErr != nil {
		return saveErr
	}
	if !canRestore {
		return nil
	}
	projectedToken, readErr := os.ReadFile(config.tokenPath)
	if readErr != nil {
		return reportCacheSideEffectError("restore", fmt.Errorf("read cache helper token: %w", readErr))
	}
	capability, exchangeErr := exchangeWorkspaceCapability(ctx, config.apiURL, strings.TrimSpace(string(projectedToken)), config.executionJobID, config.podUID, domain.WorkspaceHelperRoleCacheRestore)
	if exchangeErr != nil {
		return reportCacheSideEffectError("restore", exchangeErr)
	}
	split := splitCacheComponentsEnabled()
	for index, component := range components {
		if !cachePolicyAllowsRestore(config.componentPolicy(component.Name)) {
			continue
		}
		destination := config.root
		if split {
			destination = filepath.Join(config.root, "paths", fmt.Sprintf("%03d", index))
		}
		if restoreErr := restoreCacheComponent(ctx, config.apiURL, capability, config.executionJobID, config.podUID, destination, component, keys[component.Name]); restoreErr != nil {
			return reportCacheSideEffectError("restore", restoreErr)
		}
	}
	return nil
}

func restoreCacheComponent(ctx context.Context, apiURL, capability, executionJobID, podUID, destination string, component cachepkg.Component, key string) error {
	started := time.Now()
	body, marshalErr := json.Marshal(api.WorkspaceHelperCacheRequest{ExecutionJobID: executionJobID, PodUID: podUID, Preset: component.Name, CacheKey: key})
	if marshalErr != nil {
		log.Printf("INFO cache_transfer operation=restore_client outcome=request_error preset=%s cache_key=%s total_ms=%d", component.Name, key, time.Since(started).Milliseconds())
		return marshalErr
	}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/cache/restore", bytes.NewReader(body))
	if requestErr != nil {
		log.Printf("INFO cache_transfer operation=restore_client outcome=request_error preset=%s cache_key=%s total_ms=%d", component.Name, key, time.Since(started).Milliseconds())
		return requestErr
	}
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/json")
	requestStarted := time.Now()
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		log.Printf("INFO cache_transfer operation=restore_client outcome=error preset=%s cache_key=%s http_request_ms=%d total_ms=%d", component.Name, key, time.Since(requestStarted).Milliseconds(), time.Since(started).Milliseconds())
		return doErr
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNoContent {
		log.Printf("INFO cache_transfer operation=restore_client outcome=miss preset=%s cache_key=%s http_request_ms=%d total_ms=%d", component.Name, key, time.Since(requestStarted).Milliseconds(), time.Since(started).Milliseconds())
		return nil
	}
	if response.StatusCode != http.StatusOK {
		restoreErr := cacheHelperHTTPError("restore", response)
		log.Printf("INFO cache_transfer operation=restore_client outcome=restore_error preset=%s cache_key=%s http_request_ms=%d total_ms=%d error=%v", component.Name, key, time.Since(requestStarted).Milliseconds(), time.Since(started).Milliseconds(), restoreErr)
		return restoreErr
	}
	size := response.ContentLength
	publication := domain.WorkspaceRevisionPublication{StorageKey: "cache/transport.tar.gz", ContentDigest: strings.TrimSpace(response.Header.Get("Content-Digest")), SizeBytes: &size}
	timedBody := &timedReadCloser{ReadCloser: response.Body}
	if !splitCacheComponentsEnabled() {
		extractStarted := time.Now()
		restoreErr := restoreCacheArchive(ctx, timedBody, publication, destination, component.Name, strings.TrimSpace(os.Getenv(cacheHelperWorkingDir)))
		logRestoreCacheTransfer(component.Name, key, size, time.Since(requestStarted), time.Since(extractStarted), timedBody.readDuration, time.Since(started), restoreErr)
		return restoreErr
	}
	extractStarted := time.Now()
	restoreErr := workspacepkg.RestoreArchive(ctx, timedBody, publication, destination)
	logRestoreCacheTransfer(component.Name, key, size, time.Since(requestStarted), time.Since(extractStarted), timedBody.readDuration, time.Since(started), restoreErr)
	return restoreErr
}

func logRestoreCacheTransfer(preset, key string, size int64, requestDuration, extractDuration, readDuration, total time.Duration, restoreErr error) {
	outcome := "hit"
	if restoreErr != nil {
		outcome = "restore_error"
	}
	processingDuration := extractDuration - readDuration
	if processingDuration < 0 {
		processingDuration = 0
	}
	log.Printf("INFO cache_transfer operation=restore_client outcome=%s preset=%s cache_key=%s compressed_bytes=%d http_request_ms=%d response_body_read_ms=%d gzip_tar_extract_and_fs_ms=%d total_ms=%d error=%v", outcome, preset, key, size, requestDuration.Milliseconds(), readDuration.Milliseconds(), processingDuration.Milliseconds(), total.Milliseconds(), restoreErr)
}

type timedReadCloser struct {
	io.ReadCloser
	readDuration time.Duration
}

func (r *timedReadCloser) Read(data []byte) (int, error) {
	started := time.Now()
	read, err := r.ReadCloser.Read(data)
	r.readDuration += time.Since(started)
	return read, err
}

func restoreCacheArchive(ctx context.Context, archive io.Reader, publication domain.WorkspaceRevisionPublication, root string, preset string, workingDir string) error {
	if clearErr := clearCacheRoot(root); clearErr != nil {
		return clearErr
	}
	restoreErr := workspacepkg.RestoreArchive(ctx, archive, publication, root)
	mountErr := ensureCacheMountPaths(root, preset, workingDir)
	if restoreErr != nil {
		return restoreErr
	}
	return mountErr
}

func clearCacheRoot(root string) error {
	rootPath := strings.TrimSpace(root)
	if rootPath == "" {
		return errors.New("cache root is required")
	}
	rootInfo, statErr := os.Lstat(rootPath)
	if statErr != nil {
		return statErr
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return errors.New("cache root must be a directory")
	}
	entries, readErr := os.ReadDir(rootPath)
	if readErr != nil {
		return readErr
	}
	for _, entry := range entries {
		if removeErr := os.RemoveAll(filepath.Join(rootPath, entry.Name())); removeErr != nil {
			return removeErr
		}
	}
	return nil
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
	config, err := cacheHelperConfig()
	if err != nil {
		return err
	}
	components, componentErr := cacheComponentsForHelper(config.preset, strings.TrimSpace(os.Getenv(cacheHelperWorkingDir)))
	if componentErr != nil {
		return reportCacheSideEffectError("save", componentErr)
	}
	canSave := false
	for _, component := range components {
		canSave = canSave || cachePolicyAllowsSave(config.componentPolicy(component.Name))
	}
	if !canSave {
		return nil
	}
	keys, keyErr := loadPreparedCacheKeys(config.stateRoot)
	if keyErr != nil {
		return reportCacheSideEffectError("save", keyErr)
	}
	projectedToken, readErr := os.ReadFile(config.tokenPath)
	if readErr != nil {
		return reportCacheSideEffectError("save", fmt.Errorf("read cache helper token: %w", readErr))
	}
	capability, exchangeErr := exchangeWorkspaceCapability(ctx, config.apiURL, strings.TrimSpace(string(projectedToken)), config.executionJobID, config.podUID, domain.WorkspaceHelperRoleCacheSave)
	if exchangeErr != nil {
		return reportCacheSideEffectError("save", exchangeErr)
	}
	split := splitCacheComponentsEnabled()
	for index, component := range components {
		if !cachePolicyAllowsSave(config.componentPolicy(component.Name)) {
			continue
		}
		source := config.root
		if split {
			source = filepath.Join(config.root, "paths", fmt.Sprintf("%03d", index))
		}
		if saveErr := saveCacheComponent(ctx, config.apiURL, capability, config.executionJobID, config.podUID, source, component.Name, keys[component.Name]); saveErr != nil {
			return reportCacheSideEffectError("save", saveErr)
		}
	}
	return nil
}

func saveCacheComponent(ctx context.Context, apiURL, capability, executionJobID, podUID, source, preset, key string) error {
	started := time.Now()
	preclaimStarted := time.Now()
	acquired, claimToken, preclaimErr := claimCachePublish(ctx, apiURL, capability, executionJobID, podUID, preset, key)
	preclaimDuration := time.Since(preclaimStarted)
	if preclaimErr != nil {
		log.Printf("INFO cache_transfer operation=save_client outcome=preclaim_error preclaim_outcome=error preset=%s cache_key=%s preclaim_ms=%d archive_skipped=true claim_lifecycle=not_acquired total_ms=%d", preset, key, preclaimDuration.Milliseconds(), time.Since(started).Milliseconds())
		return preclaimErr
	}
	if !acquired {
		log.Printf("INFO cache_transfer operation=save_client outcome=deduplicated_publish preclaim_outcome=not_acquired preset=%s cache_key=%s preclaim_ms=%d archive_skipped=true claim_lifecycle=not_acquired total_ms=%d", preset, key, preclaimDuration.Milliseconds(), time.Since(started).Milliseconds())
		return nil
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		if releaseErr := releaseCachePublishClaim(context.Background(), apiURL, capability, executionJobID, podUID, preset, key, claimToken); releaseErr != nil {
			log.Printf("WARN cache_transfer operation=save_client outcome=claim_release_error preset=%s cache_key=%s claim_lifecycle=release_failed error=%v", preset, key, releaseErr)
		}
	}()
	archiveStarted := time.Now()
	archive, publication, archiveErr := archiveCacheDirectory(ctx, source)
	archiveCreationDuration := time.Since(archiveStarted)
	if archiveErr != nil {
		log.Printf("INFO cache_transfer operation=save_client outcome=archive_error preclaim_outcome=acquired preset=%s cache_key=%s preclaim_ms=%d archive_skipped=false claim_lifecycle=expires_after_archive_error gzip_tar_create_and_fs_ms=%d total_ms=%d", preset, key, preclaimDuration.Milliseconds(), archiveCreationDuration.Milliseconds(), time.Since(started).Milliseconds())
		return archiveErr
	}
	defer func() { _ = archive.Close() }()
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/cache/save", archive)
	if requestErr != nil {
		log.Printf("INFO cache_transfer operation=save_client outcome=request_error preclaim_outcome=acquired preset=%s cache_key=%s compressed_bytes=%d preclaim_ms=%d archive_skipped=false claim_lifecycle=expires_after_request_error gzip_tar_create_and_fs_ms=%d total_ms=%d", preset, key, *publication.SizeBytes, preclaimDuration.Milliseconds(), archiveCreationDuration.Milliseconds(), time.Since(started).Milliseconds())
		return requestErr
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
	request.Header.Set("Coyote-Cache-Claim-Token", claimToken)
	uploadStarted := time.Now()
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		log.Printf("INFO cache_transfer operation=save_client outcome=upload_error preclaim_outcome=acquired preset=%s cache_key=%s compressed_bytes=%d preclaim_ms=%d archive_skipped=false claim_lifecycle=unknown_after_transport_error gzip_tar_create_and_fs_ms=%d http_upload_and_finalize_ms=%d total_ms=%d", preset, key, *publication.SizeBytes, preclaimDuration.Milliseconds(), archiveCreationDuration.Milliseconds(), time.Since(uploadStarted).Milliseconds(), time.Since(started).Milliseconds())
		return doErr
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusNoContent {
		log.Printf("INFO cache_transfer operation=save_client outcome=upload_error preclaim_outcome=acquired preset=%s cache_key=%s compressed_bytes=%d preclaim_ms=%d archive_skipped=false claim_lifecycle=rejected gzip_tar_create_and_fs_ms=%d http_upload_and_finalize_ms=%d total_ms=%d", preset, key, *publication.SizeBytes, preclaimDuration.Milliseconds(), archiveCreationDuration.Milliseconds(), time.Since(uploadStarted).Milliseconds(), time.Since(started).Milliseconds())
		return cacheHelperHTTPError("save", response)
	}
	completed = true
	log.Printf("INFO cache_transfer operation=save_client outcome=complete preclaim_outcome=acquired preset=%s cache_key=%s compressed_bytes=%d preclaim_ms=%d archive_skipped=false claim_lifecycle=completed gzip_tar_create_and_fs_ms=%d http_upload_and_finalize_ms=%d total_ms=%d", preset, key, *publication.SizeBytes, preclaimDuration.Milliseconds(), archiveCreationDuration.Milliseconds(), time.Since(uploadStarted).Milliseconds(), time.Since(started).Milliseconds())
	return nil
}

func claimCachePublish(ctx context.Context, apiURL, capability, executionJobID, podUID, preset, key string) (bool, string, error) {
	body, marshalErr := json.Marshal(api.WorkspaceHelperCacheRequest{ExecutionJobID: executionJobID, PodUID: podUID, Preset: preset, CacheKey: key})
	if marshalErr != nil {
		return false, "", marshalErr
	}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/cache/publish-claim", bytes.NewReader(body))
	if requestErr != nil {
		return false, "", requestErr
	}
	request.Header.Set("Authorization", "Bearer "+capability)
	request.Header.Set("Content-Type", "application/json")
	response, doErr := http.DefaultClient.Do(request)
	if doErr != nil {
		return false, "", doErr
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return false, "", cacheHelperHTTPError("publish claim", response)
	}
	var payload struct {
		Data api.WorkspaceHelperCachePublishClaimResponse `json:"data"`
	}
	if decodeErr := json.NewDecoder(response.Body).Decode(&payload); decodeErr != nil {
		return false, "", decodeErr
	}
	switch payload.Data.Outcome {
	case "acquired":
		if strings.TrimSpace(payload.Data.ClaimToken) == "" {
			return false, "", errors.New("cache publish claim response omitted claim token")
		}
		return true, payload.Data.ClaimToken, nil
	case "not_acquired":
		return false, "", nil
	default:
		return false, "", fmt.Errorf("cache publish claim returned unknown outcome %q", payload.Data.Outcome)
	}
}

func releaseCachePublishClaim(ctx context.Context, apiURL, capability, executionJobID, podUID, preset, key, claimToken string) error {
	body, marshalErr := json.Marshal(api.WorkspaceHelperCacheRequest{ExecutionJobID: executionJobID, PodUID: podUID, Preset: preset, CacheKey: key, ClaimToken: claimToken})
	if marshalErr != nil {
		return marshalErr
	}
	request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/api/internal/workspace-helper/cache/publish-claim/release", bytes.NewReader(body))
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
	if response.StatusCode != http.StatusNoContent {
		return cacheHelperHTTPError("publish claim release", response)
	}
	return nil
}

func cacheHelperHTTPError(operation string, response *http.Response) error {
	if response == nil {
		return fmt.Errorf("cache %s received no HTTP response", operation)
	}
	message := fmt.Sprintf("cache %s returned HTTP %d", operation, response.StatusCode)
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	if response.Body == nil || (!strings.HasPrefix(contentType, "application/json") && !strings.HasPrefix(contentType, "text/")) {
		return errors.New(message)
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxCacheHelperErrorResponseBytes+1))
	if readErr != nil || !utf8.Valid(body) {
		return errors.New(message)
	}
	truncated := len(body) > maxCacheHelperErrorResponseBytes
	if truncated {
		body = body[:maxCacheHelperErrorResponseBytes]
	}
	if detail := strings.TrimSpace(string(body)); detail != "" {
		if truncated {
			detail += "... (truncated)"
		}
		return fmt.Errorf("%s: %s", message, detail)
	}
	return errors.New(message)
}

func reportCacheSideEffectError(operation string, err error) error {
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "cache %s failed: %v\n", operation, err)
	}
	return nil
}

type cacheHelperConfiguration struct {
	apiURL            string
	tokenPath         string
	executionJobID    string
	podUID            string
	root              string
	preset            string
	stateRoot         string
	policy            domain.CachePolicy
	componentPolicies map[string]domain.CachePolicy
}

func (c cacheHelperConfiguration) componentPolicy(component string) domain.CachePolicy {
	if policy, ok := c.componentPolicies[component]; ok {
		return policy
	}
	return c.policy
}

func cacheHelperConfig() (cacheHelperConfiguration, error) {
	config := cacheHelperConfiguration{
		apiURL:         strings.TrimRight(strings.TrimSpace(os.Getenv(workspaceHelperAPIURL)), "/"),
		tokenPath:      strings.TrimSpace(os.Getenv(workspaceHelperTokenPath)),
		executionJobID: strings.TrimSpace(os.Getenv(workspaceHelperExecutionJobID)),
		podUID:         strings.TrimSpace(os.Getenv(workspaceHelperPodUID)),
		root:           strings.TrimSpace(os.Getenv(cacheHelperRoot)),
		preset:         strings.TrimSpace(os.Getenv(cacheHelperPreset)),
		stateRoot:      strings.TrimSpace(os.Getenv(cacheHelperStateRoot)),
		policy:         domain.NormalizeCachePolicy(domain.CachePolicy(os.Getenv(cacheHelperPolicy))),
	}
	if config.apiURL == "" || config.tokenPath == "" || config.executionJobID == "" || config.podUID == "" || config.root == "" || config.preset == "" || config.stateRoot == "" {
		return cacheHelperConfiguration{}, errors.New("cache helper requires internal API URL, token path, execution job ID, pod UID, root, preset, and state root")
	}
	componentPolicies, policyErr := cacheComponentPolicies()
	if policyErr != nil {
		return cacheHelperConfiguration{}, policyErr
	}
	config.componentPolicies = componentPolicies
	return config, nil
}

func cacheComponentPolicies() (map[string]domain.CachePolicy, error) {
	raw := strings.TrimSpace(os.Getenv(cacheHelperComponentPolicies))
	if raw == "" {
		return nil, nil
	}
	values := map[string]string{}
	if unmarshalErr := json.Unmarshal([]byte(raw), &values); unmarshalErr != nil {
		return nil, fmt.Errorf("parse cache component policies: %w", unmarshalErr)
	}
	if len(values) == 0 {
		return nil, errors.New("cache component policies cannot be empty")
	}
	policies := make(map[string]domain.CachePolicy, len(values))
	for component, value := range values {
		if strings.TrimSpace(component) == "" || !cachepkg.IsSupportedPolicy(value) || strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("invalid cache component policy for %q", component)
		}
		policies[component] = domain.NormalizeCachePolicy(domain.CachePolicy(value))
	}
	return policies, nil
}

func cachePolicyAllowsRestore(policy domain.CachePolicy) bool {
	return policy != domain.CachePolicyOff && policy != domain.CachePolicyPush
}

func cachePolicyAllowsSave(policy domain.CachePolicy) bool {
	return policy != domain.CachePolicyOff && policy != domain.CachePolicyPull
}

func savePreparedCacheKey(stateRoot string, key string) error {
	return savePreparedCacheKeys(stateRoot, map[string]string{"legacy": key})
}

func savePreparedCacheKeys(stateRoot string, keys map[string]string) error {
	if mkdirErr := os.MkdirAll(stateRoot, 0o755); mkdirErr != nil {
		return fmt.Errorf("create cache helper state directory: %w", mkdirErr)
	}
	payload, marshalErr := json.Marshal(keys)
	if marshalErr != nil {
		return marshalErr
	}
	if writeErr := os.WriteFile(filepath.Join(stateRoot, "prepared-keys"), payload, 0o600); writeErr != nil {
		return fmt.Errorf("write prepared cache key: %w", writeErr)
	}
	return nil
}

func loadPreparedCacheKey(stateRoot string) (string, error) {
	keys, err := loadPreparedCacheKeys(stateRoot)
	if err != nil {
		return "", err
	}
	if key := keys["legacy"]; key != "" {
		return key, nil
	}
	if key := keys["go"]; key != "" {
		return key, nil
	}
	return "", errors.New("prepared cache key is empty")
}

func loadPreparedCacheKeys(stateRoot string) (map[string]string, error) {
	contents, readErr := os.ReadFile(filepath.Join(stateRoot, "prepared-keys"))
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			return nil, fmt.Errorf("read prepared cache key: %w", readErr)
		}
		legacy, legacyErr := os.ReadFile(filepath.Join(stateRoot, "prepared-key"))
		if legacyErr != nil {
			return nil, fmt.Errorf("read prepared cache key: %w", readErr)
		}
		return map[string]string{"go": strings.TrimSpace(string(legacy))}, nil
	}
	keys := map[string]string{}
	if unmarshalErr := json.Unmarshal(contents, &keys); unmarshalErr != nil {
		return nil, unmarshalErr
	}
	if len(keys) == 0 {
		return nil, errors.New("prepared cache keys are empty")
	}
	return keys, nil
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

func cacheKeysForHelper(components []cachepkg.Component) (map[string]string, error) {
	if !splitCacheComponentsEnabled() {
		key, err := cacheKeyForHelper("", components[0].Name)
		if err != nil {
			return nil, err
		}
		return map[string]string{components[0].Name: key}, nil
	}
	workspaceRoot := strings.TrimSpace(os.Getenv(workspaceHelperWorkspacePath))
	keys := make(map[string]string, len(components))
	for _, component := range components {
		fingerprint, _, err := cachepkg.ComputeFingerprint(workspaceRoot, component.FingerprintFiles)
		if err != nil {
			return nil, err
		}
		dimensions := append([]string(nil), component.KeyDimensions...)
		if component.Name == "go-build" {
			buildImage := strings.TrimSpace(os.Getenv(cacheHelperBuildImage))
			if buildImage == "" {
				return nil, errors.New("cache helper requires build image identity for go-build cache")
			}
			dimensions = append(dimensions, buildImage)
		}
		identity := fingerprint + "\n" + strings.Join(dimensions, "\n")
		digest := sha256.Sum256([]byte(identity))
		keys[component.Name] = component.Name + ":" + hex.EncodeToString(digest[:])
	}
	return keys, nil
}

func cacheComponentsForHelper(preset string, workingDir string) ([]cachepkg.Component, error) {
	if splitCacheComponentsEnabled() {
		return cachepkg.ResolvePresetComponents(preset, workingDir)
	}
	resolved, err := cachepkg.ResolvePreset(preset, workingDir)
	if err != nil {
		return nil, err
	}
	return []cachepkg.Component{{Name: resolved.Name, CachePath: resolved.CachePaths[0], FingerprintFiles: resolved.FingerprintFiles}}, nil
}

func splitCacheComponentsEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(cacheHelperComponents)), "split")
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
