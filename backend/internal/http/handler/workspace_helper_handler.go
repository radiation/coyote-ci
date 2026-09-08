package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/radiation/coyote-ci/backend/internal/api"
	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/repository"
	"github.com/radiation/coyote-ci/backend/internal/service"
	"github.com/radiation/coyote-ci/backend/internal/workspace"
)

type workspaceHelperCapabilityExchanger interface {
	Exchange(context.Context, string, domain.WorkspaceHelperCapability) (string, domain.WorkspaceHelperCapability, error)
}

type workspacePrepareOpener interface {
	Open(context.Context, string, string, string) (service.WorkspacePreparePayload, error)
}

type workspacePublisher interface {
	Publish(context.Context, string, string, string, io.Reader) (domain.WorkspaceRevision, error)
}

type workspaceCacheHelper interface {
	Restore(context.Context, string, string, string, string, string) (service.WorkspaceHelperCachePayload, bool, error)
	Save(context.Context, string, string, string, string, string, io.Reader, domain.WorkspaceRevisionPublication) error
}

type WorkspaceHelperHandler struct {
	capabilities        workspaceHelperCapabilityExchanger
	prepare             workspacePrepareOpener
	publish             workspacePublisher
	cache               workspaceCacheHelper
	cacheMaxUploadBytes int64
}

func NewWorkspaceHelperHandler(capabilities workspaceHelperCapabilityExchanger) *WorkspaceHelperHandler {
	return &WorkspaceHelperHandler{capabilities: capabilities}
}

func (h *WorkspaceHelperHandler) SetPrepareService(prepare workspacePrepareOpener) {
	if h != nil {
		h.prepare = prepare
	}
}

func (h *WorkspaceHelperHandler) SetPublishService(publish workspacePublisher) {
	if h != nil {
		h.publish = publish
	}
}

func (h *WorkspaceHelperHandler) SetCacheService(cache workspaceCacheHelper) {
	if h != nil {
		h.cache = cache
	}
}

func (h *WorkspaceHelperHandler) SetCacheMaxUploadBytes(maxBytes int64) {
	if h != nil {
		h.cacheMaxUploadBytes = maxBytes
	}
}

func (h *WorkspaceHelperHandler) PrepareCapabilityAuthorizer() service.WorkspacePrepareCapabilityAuthorizer {
	if h == nil {
		return nil
	}
	authorizer, _ := h.capabilities.(service.WorkspacePrepareCapabilityAuthorizer)
	return authorizer
}

func (h *WorkspaceHelperHandler) ExchangeCapability(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.capabilities == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "unavailable", "workspace helper exchange is not configured")
		return
	}
	projectedToken, ok := bearerToken(r)
	if !ok || projectedToken == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workload identity is required")
		return
	}
	var request api.WorkspaceHelperCapabilityExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	capability := domain.WorkspaceHelperCapability{ExecutionJobID: strings.TrimSpace(request.ExecutionJobID), PodUID: strings.TrimSpace(request.PodUID), Role: domain.WorkspaceHelperRole(strings.TrimSpace(request.Role))}
	token, issued, err := h.capabilities.Exchange(r.Context(), projectedToken, capability)
	if err != nil {
		if errors.Is(err, service.ErrWorkspaceHelperUnauthorized) || errors.Is(err, service.ErrWorkspaceHelperCapabilityMalformed) {
			log.Printf("WARN workspace helper capability denied execution_job_id=%s pod_uid=%s role=%s", capability.ExecutionJobID, capability.PodUID, capability.Role)
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper authorization failed")
			return
		}
		log.Printf("ERROR workspace helper capability exchange failed execution_job_id=%s pod_uid=%s role=%s err=%v", capability.ExecutionJobID, capability.PodUID, capability.Role, err)
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	log.Printf("INFO workspace helper capability issued execution_job_id=%s pod_uid=%s role=%s", issued.ExecutionJobID, issued.PodUID, issued.Role)
	writeDataJSON(w, http.StatusCreated, api.WorkspaceHelperCapabilityExchangeResponse{Capability: token, ExpiresAt: issued.ExpiresAt.UTC().Format(time.RFC3339)})
}

func (h *WorkspaceHelperHandler) PrepareWorkspace(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.prepare == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "unavailable", "workspace prepare is not configured")
		return
	}
	capability, ok := bearerToken(r)
	if !ok || capability == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper capability is required")
		return
	}
	var request api.WorkspaceHelperPrepareRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&request); decodeErr != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	prepared, openErr := h.prepare.Open(r.Context(), capability, strings.TrimSpace(request.ExecutionJobID), strings.TrimSpace(request.PodUID))
	if openErr != nil {
		if errors.Is(openErr, service.ErrWorkspaceHelperUnauthorized) {
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper authorization failed")
			return
		}
		log.Printf("ERROR workspace prepare failed execution_job_id=%s pod_uid=%s err=%v", strings.TrimSpace(request.ExecutionJobID), strings.TrimSpace(request.PodUID), openErr)
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace prepare failed")
		return
	}
	if prepared.Archive == nil || prepared.Publication.Validate() != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace prepare failed")
		return
	}
	defer func() { _ = prepared.Archive.Close() }()
	temporary, createErr := os.CreateTemp("", "coyote-workspace-prepare-*")
	if createErr != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace prepare failed")
		return
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	size, copyErr := io.Copy(temporary, prepared.Archive)
	if closeErr := temporary.Close(); copyErr != nil || closeErr != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace prepare failed")
		return
	}
	if prepared.Publication.SizeBytes == nil || size != *prepared.Publication.SizeBytes {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace prepare failed")
		return
	}
	payload, openErr := os.Open(temporaryPath)
	if openErr != nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace prepare failed")
		return
	}
	defer func() { _ = payload.Close() }()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Digest", prepared.Publication.ContentDigest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", *prepared.Publication.SizeBytes))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, payload)
}

func (h *WorkspaceHelperHandler) PublishWorkspace(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.publish == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "unavailable", "workspace publish is not configured")
		return
	}
	capability, ok := bearerToken(r)
	if !ok || capability == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper capability is required")
		return
	}
	executionJobID := strings.TrimSpace(r.Header.Get("Coyote-Execution-Job-ID"))
	podUID := strings.TrimSpace(r.Header.Get("Coyote-Pod-UID"))
	published, publishErr := h.publish.Publish(r.Context(), capability, executionJobID, podUID, r.Body)
	if publishErr != nil {
		if errors.Is(publishErr, service.ErrWorkspaceHelperUnauthorized) || errors.Is(publishErr, repository.ErrWorkspaceRevisionStaleClaim) {
			writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper authorization failed")
			return
		}
		if errors.Is(publishErr, service.ErrWorkspacePublishInvalidArchive) {
			writeErrorJSON(w, http.StatusBadRequest, "invalid_archive", "workspace archive is invalid")
			return
		}
		if errors.Is(publishErr, service.ErrWorkspacePublishArchiveTooLarge) {
			writeErrorJSON(w, http.StatusRequestEntityTooLarge, "archive_too_large", "workspace archive exceeds the configured size limit")
			return
		}
		if errors.Is(publishErr, repository.ErrWorkspaceRevisionConflict) || errors.Is(publishErr, workspace.ErrWorkspaceRevisionConflict) {
			writeErrorJSON(w, http.StatusConflict, "conflict", "workspace publication conflicts with an existing revision")
			return
		}
		log.Printf("ERROR workspace publish failed execution_job_id=%s pod_uid=%s err=%v", executionJobID, podUID, publishErr)
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace publication failed")
		return
	}
	if published.ContentDigest == nil || published.SizeBytes == nil {
		writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace publication failed")
		return
	}
	writeDataJSON(w, http.StatusOK, api.WorkspaceHelperPublishResponse{RevisionID: published.ID, ContentDigest: *published.ContentDigest, SizeBytes: *published.SizeBytes})
}

func (h *WorkspaceHelperHandler) RestoreCache(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.cache == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "unavailable", "workspace cache is not configured")
		return
	}
	capability, ok := bearerToken(r)
	if !ok || capability == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper capability is required")
		return
	}
	var request api.WorkspaceHelperCacheRequest
	if decodeErr := json.NewDecoder(r.Body).Decode(&request); decodeErr != nil {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	payload, found, restoreErr := h.cache.Restore(r.Context(), capability, strings.TrimSpace(request.ExecutionJobID), strings.TrimSpace(request.PodUID), strings.TrimSpace(request.Preset), strings.TrimSpace(request.CacheKey))
	if restoreErr != nil {
		handleWorkspaceCacheError(w, restoreErr)
		return
	}
	if !found {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	defer func() { _ = payload.Archive.Close() }()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Digest", payload.Publication.ContentDigest)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", *payload.Publication.SizeBytes))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, payload.Archive)
}

func (h *WorkspaceHelperHandler) SaveCache(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.cache == nil {
		writeErrorJSON(w, http.StatusServiceUnavailable, "unavailable", "workspace cache is not configured")
		return
	}
	capability, ok := bearerToken(r)
	if !ok || capability == "" {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper capability is required")
		return
	}
	if h.cacheMaxUploadBytes > 0 && r.ContentLength > h.cacheMaxUploadBytes {
		writeErrorJSON(w, http.StatusRequestEntityTooLarge, "archive_too_large", "cache archive exceeds the configured size limit")
		return
	}
	body := r.Body
	var limitedBody *cacheUploadLimitReader
	if h.cacheMaxUploadBytes > 0 {
		limitedBody = &cacheUploadLimitReader{ReadCloser: http.MaxBytesReader(w, r.Body, h.cacheMaxUploadBytes)}
		body = limitedBody
	}
	size := r.ContentLength
	publication := domain.WorkspaceRevisionPublication{StorageKey: "cache/transport.tar.gz", ContentDigest: strings.TrimSpace(r.Header.Get("Content-Digest")), SizeBytes: &size}
	saveErr := h.cache.Save(r.Context(), capability, strings.TrimSpace(r.Header.Get("Coyote-Execution-Job-ID")), strings.TrimSpace(r.Header.Get("Coyote-Pod-UID")), strings.TrimSpace(r.Header.Get("Coyote-Cache-Preset")), strings.TrimSpace(r.Header.Get("Coyote-Cache-Key")), body, publication)
	if saveErr != nil {
		if limitedBody != nil && limitedBody.exceeded {
			writeErrorJSON(w, http.StatusRequestEntityTooLarge, "archive_too_large", "cache archive exceeds the configured size limit")
			return
		}
		handleWorkspaceCacheError(w, saveErr)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type cacheUploadLimitReader struct {
	io.ReadCloser
	exceeded bool
}

func (r *cacheUploadLimitReader) Read(buffer []byte) (int, error) {
	read, err := r.ReadCloser.Read(buffer)
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		r.exceeded = true
	}
	return read, err
}

func handleWorkspaceCacheError(w http.ResponseWriter, err error) {
	if errors.Is(err, service.ErrWorkspaceHelperUnauthorized) {
		writeErrorJSON(w, http.StatusUnauthorized, "unauthorized", "workspace helper authorization failed")
		return
	}
	if errors.Is(err, service.ErrWorkspaceHelperCacheInvalidInput) {
		writeErrorJSON(w, http.StatusBadRequest, "invalid_request", "invalid cache request")
		return
	}
	log.Printf("ERROR workspace cache operation failed: %v", err)
	writeErrorJSON(w, http.StatusInternalServerError, "internal_error", "workspace cache operation failed")
}

func bearerToken(r *http.Request) (string, bool) {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	return parts[1], true
}
