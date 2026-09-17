package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/storage"
	"github.com/radiation/coyote-ci/backend/internal/domain"
)

func TestNewGCSStore_ValidatesConfigAndTrimsPrefix(t *testing.T) {
	_, nilClientErr := NewGCSStore(nil, GCSStoreConfig{Bucket: "bucket"})
	if nilClientErr == nil {
		t.Fatal("expected nil client error")
	}
	if !strings.Contains(nilClientErr.Error(), "client") {
		t.Fatalf("expected client error, got %v", nilClientErr)
	}

	_, missingBucketErr := NewGCSStore(&storage.Client{}, GCSStoreConfig{Bucket: " "})
	if missingBucketErr == nil {
		t.Fatal("expected missing bucket error")
	}
	if !strings.Contains(missingBucketErr.Error(), "bucket") {
		t.Fatalf("expected bucket error, got %v", missingBucketErr)
	}

	store, createErr := NewGCSStore(&storage.Client{}, GCSStoreConfig{Bucket: " cache-bucket ", Prefix: " /team/cache/ "})
	if createErr != nil {
		t.Fatalf("create gcs cache store: %v", createErr)
	}
	if store.Provider() != domain.StorageProviderGCS {
		t.Fatalf("expected gcs provider, got %q", store.Provider())
	}
	if store.bucket != "cache-bucket" || store.prefix != "team/cache" {
		t.Fatalf("expected trimmed bucket/prefix, got bucket=%q prefix=%q", store.bucket, store.prefix)
	}
}

func TestGCSStore_ObjectKeyAddsPrefixAndArchiveSuffix(t *testing.T) {
	store := &GCSStore{bucket: "bucket", prefix: "team/cache"}
	if got := store.objectKey(" /v1/jobs/job-1/key/ "); got != "team/cache/v1/jobs/job-1/key.tar.gz" {
		t.Fatalf("unexpected prefixed object key: %q", got)
	}

	withoutPrefix := &GCSStore{bucket: "bucket"}
	if got := withoutPrefix.objectKey("v1/jobs/job-1/key"); got != "v1/jobs/job-1/key.tar.gz" {
		t.Fatalf("unexpected unprefixed object key: %q", got)
	}
}

func TestGCSStore_ArchiveRoundTrip(t *testing.T) {
	emulator := newGCSTestEmulator(t)
	t.Setenv("STORAGE_EMULATOR_HOST", emulator.URL)
	ctx := context.Background()
	client, clientErr := storage.NewClient(ctx)
	if clientErr != nil {
		t.Fatalf("create emulator client: %v", clientErr)
	}
	defer func() { _ = client.Close() }()
	store, storeErr := NewGCSStore(client, GCSStoreConfig{Bucket: "cache-bucket", Prefix: "cache"})
	if storeErr != nil {
		t.Fatalf("create store: %v", storeErr)
	}

	archive := []byte("cache archive")
	saved, saveErr := store.SaveArchive(ctx, "source", bytes.NewReader(archive))
	if saveErr != nil {
		t.Fatalf("save archive: %v", saveErr)
	}
	wantDigest := sha256.Sum256(archive)
	if saved.SizeBytes != int64(len(archive)) || saved.Checksum != hex.EncodeToString(wantDigest[:]) || saved.Compression != "tar.gz" {
		t.Fatalf("save result=%+v", saved)
	}

	reader, restored, openErr := store.Open(ctx, "source")
	if openErr != nil || !restored.Hit || restored.SizeBytes != int64(len(archive)) {
		t.Fatalf("open result=%+v err=%v", restored, openErr)
	}
	contents, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(contents, archive) {
		t.Fatalf("read=%q readErr=%v closeErr=%v", contents, readErr, closeErr)
	}

	if promoteErr := store.PromoteArchive(ctx, "source", "promoted"); promoteErr != nil {
		t.Fatalf("promote archive: %v", promoteErr)
	}
	promoted, promotedResult, promotedErr := store.Open(ctx, "promoted")
	if promotedErr != nil || !promotedResult.Hit {
		t.Fatalf("open promoted result=%+v err=%v", promotedResult, promotedErr)
	}
	promotedContents, promotedReadErr := io.ReadAll(promoted)
	promotedCloseErr := promoted.Close()
	if promotedReadErr != nil || promotedCloseErr != nil || !bytes.Equal(promotedContents, archive) {
		t.Fatalf("promoted contents=%q readErr=%v closeErr=%v", promotedContents, promotedReadErr, promotedCloseErr)
	}

	if deleteErr := store.DeleteArchive(ctx, "promoted"); deleteErr != nil {
		t.Fatalf("delete archive: %v", deleteErr)
	}
	_, missing, missingErr := store.Open(ctx, "promoted")
	if missingErr != nil || missing.Hit {
		t.Fatalf("missing result=%+v err=%v", missing, missingErr)
	}
	if deleteErr := store.DeleteArchive(ctx, "promoted"); deleteErr != nil {
		t.Fatalf("delete missing archive: %v", deleteErr)
	}
}

type gcsTestEmulator struct {
	*httptest.Server
	mu      sync.Mutex
	objects map[string][]byte
}

func newGCSTestEmulator(t *testing.T) *gcsTestEmulator {
	t.Helper()
	emulator := &gcsTestEmulator{objects: make(map[string][]byte)}
	emulator.Server = httptest.NewServer(http.HandlerFunc(emulator.serveHTTP))
	t.Cleanup(emulator.Close)
	return emulator
}

func (e *gcsTestEmulator) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	objectKey := strings.TrimPrefix(request.URL.Path, "/download/storage/v1/b/cache-bucket/o/")
	objectKey = strings.TrimPrefix(objectKey, "/storage/v1/b/cache-bucket/o/")
	objectKey = strings.TrimPrefix(objectKey, "/cache-bucket/")
	objectKey, _ = url.PathUnescape(objectKey)
	if request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/upload/storage/v1/b/cache-bucket/o") {
		name := request.URL.Query().Get("name")
		contents, readErr := io.ReadAll(request.Body)
		if readErr != nil {
			http.Error(writer, readErr.Error(), http.StatusInternalServerError)
			return
		}
		if mediaType, parameters, parseErr := mime.ParseMediaType(request.Header.Get("Content-Type")); parseErr == nil && strings.HasPrefix(mediaType, "multipart/") {
			multipartReader := multipart.NewReader(bytes.NewReader(contents), parameters["boundary"])
			for {
				part, partErr := multipartReader.NextPart()
				if errors.Is(partErr, io.EOF) {
					break
				}
				if partErr != nil {
					http.Error(writer, partErr.Error(), http.StatusBadRequest)
					return
				}
				partContents, partReadErr := io.ReadAll(part)
				if partReadErr != nil {
					http.Error(writer, partReadErr.Error(), http.StatusBadRequest)
					return
				}
				if strings.HasPrefix(part.Header.Get("Content-Type"), "application/json") {
					var metadata struct{ Name string }
					if json.Unmarshal(partContents, &metadata) == nil {
						name = metadata.Name
					}
				} else {
					contents = partContents
				}
			}
		}
		e.mu.Lock()
		e.objects[name] = contents
		e.mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"bucket":"cache-bucket","name":"` + name + `","size":"` + fmt.Sprint(len(contents)) + `"}`))
		return
	}
	if request.Method == http.MethodPost && strings.Contains(request.URL.Path, "/rewriteTo/b/cache-bucket/o/") {
		parts := strings.Split(request.URL.Path, "/rewriteTo/b/cache-bucket/o/")
		source, _ := url.PathUnescape(strings.TrimPrefix(parts[0], "/storage/v1/b/cache-bucket/o/"))
		destination, _ := url.PathUnescape(parts[1])
		e.mu.Lock()
		e.objects[destination] = append([]byte(nil), e.objects[source]...)
		e.mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"done":true,"resource":{"bucket":"cache-bucket","name":"` + destination + `"}}`))
		return
	}
	e.mu.Lock()
	contents, found := e.objects[objectKey]
	if request.Method == http.MethodDelete && found {
		delete(e.objects, objectKey)
	}
	e.mu.Unlock()
	if !found {
		http.Error(writer, "not found", http.StatusNotFound)
		return
	}
	if request.Method == http.MethodDelete {
		writer.WriteHeader(http.StatusNoContent)
		return
	}
	if request.Method == http.MethodGet && (request.URL.Query().Get("alt") == "media" || strings.HasPrefix(request.URL.Path, "/cache-bucket/")) {
		writer.Header().Set("Content-Length", fmt.Sprint(len(contents)))
		_, _ = writer.Write(contents)
		return
	}
	http.Error(writer, "unexpected request", http.StatusBadRequest)
}
