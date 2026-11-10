package release

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

func TestGCSStoreManifestPublicationAndReads(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	store, storeErr := newGCSStore(objects, "releases/prod")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	manifest := validManifest()
	digest, publishErr := store.PublishManifest(context.Background(), manifest)
	if publishErr != nil {
		t.Fatalf("PublishManifest: %v", publishErr)
	}
	if object, found := objects.object("releases/prod/releases/2.5.1.json"); !found || Digest(object.data) != digest {
		t.Fatalf("stored manifest=%+v found=%t digest=%s", object, found, digest)
	}
	repeatedDigest, repeatErr := store.PublishManifest(context.Background(), manifest)
	if repeatErr != nil || repeatedDigest != digest {
		t.Fatalf("identical retry digest=%s err=%v", repeatedDigest, repeatErr)
	}
	manifest.Images["server"] = "registry.example/coyote-server@sha256:" + strings.Repeat("e", 64)
	if _, conflictErr := store.PublishManifest(context.Background(), manifest); !errors.Is(conflictErr, ErrManifestConflict) {
		t.Fatalf("conflicting retry error=%v", conflictErr)
	}
	document, fetchErr := store.FetchManifest(context.Background(), "2.5.1")
	if fetchErr != nil || document.Digest != digest {
		t.Fatalf("FetchManifest document=%+v err=%v", document, fetchErr)
	}
}

func TestGCSStoreManifestPublicationReconcilesAmbiguousWrite(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	objects.createAfterWriteErr = errors.New("connection reset")
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	manifest := validManifest()
	digest, publishErr := store.PublishManifest(context.Background(), manifest)
	if publishErr != nil || digest == "" {
		t.Fatalf("PublishManifest digest=%q err=%v", digest, publishErr)
	}
}

func TestGCSStoreManifestPublicationPreservesWriteAndReconciliationErrors(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	writeErr := errors.New("permission denied")
	reconcileErr := errors.New("read connection reset")
	objects.createErr = writeErr
	objects.readErr = reconcileErr
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	_, publishErr := store.PublishManifest(context.Background(), validManifest())
	if !errors.Is(publishErr, writeErr) || !errors.Is(publishErr, reconcileErr) {
		t.Fatalf("publish error=%v", publishErr)
	}
}

func TestGCSStoreReadErrorsAndLimits(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	if _, missingErr := store.FetchManifest(context.Background(), "2.5.1"); !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("missing error=%v", missingErr)
	}
	permissionErr := errors.New("permission denied")
	objects.readErr = permissionErr
	if _, fetchErr := store.FetchManifest(context.Background(), "2.5.1"); !errors.Is(fetchErr, permissionErr) || errors.Is(fetchErr, ErrNotFound) {
		t.Fatalf("permission error=%v", fetchErr)
	}
	objects.readErr = nil
	objects.put("releases/2.5.1.json", []byte(strings.Repeat("x", maxReleaseDocumentBytes+1)))
	if _, fetchErr := store.FetchManifest(context.Background(), "2.5.1"); fetchErr == nil {
		t.Fatal("expected oversized object error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, fetchErr := store.FetchManifest(ctx, "2.5.1"); !errors.Is(fetchErr, context.Canceled) {
		t.Fatalf("canceled fetch error=%v", fetchErr)
	}
	if _, fetchErr := store.FetchChannelIndex(context.Background()); !errors.Is(fetchErr, ErrNotFound) {
		t.Fatalf("missing channel index error=%v", fetchErr)
	}
	objects.put("channels.json", []byte("not json"))
	if _, fetchErr := store.FetchChannelIndex(context.Background()); fetchErr == nil {
		t.Fatal("expected invalid channel index error")
	}
}

func TestGCSStoreChannelPromotionUsesCompareAndSwap(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	channels, marshalErr := marshalChannelIndex(ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{}})
	if marshalErr != nil {
		t.Fatalf("marshalChannelIndex: %v", marshalErr)
	}
	objects.put("channels.json", channels)
	synchronized := &synchronizedReplaceObjectStore{
		releaseObjectStore: objects,
		replaceReady:       make(chan struct{}),
		replaceProceed:     make(chan struct{}),
	}
	first, firstErr := newGCSStore(synchronized, "")
	if firstErr != nil {
		t.Fatalf("first store: %v", firstErr)
	}
	second, secondErr := newGCSStore(synchronized, "")
	if secondErr != nil {
		t.Fatalf("second store: %v", secondErr)
	}
	latestDigest := "sha256:" + strings.Repeat("a", 64)
	stableDigest := "sha256:" + strings.Repeat("b", 64)
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	go func() {
		<-start
		errorsSeen <- first.PromoteChannel(context.Background(), "latest", "0.0.10", latestDigest)
	}()
	go func() {
		<-start
		errorsSeen <- second.PromoteChannel(context.Background(), "stable", "0.0.9", stableDigest)
	}()
	close(start)
	<-synchronized.replaceReady
	close(synchronized.replaceProceed)
	for range 2 {
		if promotionErr := <-errorsSeen; promotionErr != nil {
			t.Fatalf("PromoteChannel: %v", promotionErr)
		}
	}
	index, fetchErr := first.FetchChannelIndex(context.Background())
	if fetchErr != nil || index.Channels["latest"].ManifestDigest != latestDigest || index.Channels["stable"].ManifestDigest != stableDigest {
		t.Fatalf("index=%+v err=%v", index, fetchErr)
	}
	if synchronized.replaceCalls != 3 {
		t.Fatalf("replace calls=%d want 3 after one CAS retry", synchronized.replaceCalls)
	}
}

func TestGCSStoreChannelPromotionReconcilesAmbiguousWrite(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	initialDigest := "sha256:" + strings.Repeat("a", 64)
	updatedDigest := "sha256:" + strings.Repeat("b", 64)
	if channelErr := store.PromoteChannel(context.Background(), "latest", "0.0.10", initialDigest); channelErr != nil {
		t.Fatalf("initial promotion: %v", channelErr)
	}
	objects.replaceAfterWriteErr = errors.New("connection reset")
	if channelErr := store.PromoteChannel(context.Background(), "latest", "0.0.11", updatedDigest); channelErr != nil {
		t.Fatalf("ambiguous promotion: %v", channelErr)
	}
	index, fetchErr := store.FetchChannelIndex(context.Background())
	if fetchErr != nil || index.Channels["latest"] != (ChannelEntry{Release: "0.0.11", ManifestDigest: updatedDigest}) {
		t.Fatalf("index=%+v err=%v", index, fetchErr)
	}
}

func TestGCSStoreChannelPromotionDoesNotRollbackAfterAmbiguousWrite(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	initialDigest := "sha256:" + strings.Repeat("a", 64)
	requestedDigest := "sha256:" + strings.Repeat("b", 64)
	newerDigest := "sha256:" + strings.Repeat("c", 64)
	if channelErr := store.PromoteChannel(context.Background(), "latest", "0.0.10", initialDigest); channelErr != nil {
		t.Fatalf("initial promotion: %v", channelErr)
	}
	writeErr := errors.New("connection reset")
	objects.replaceAfterWriteErr = writeErr
	objects.replaceAfterWrite = func(s *releaseObjectStoreFake, name string) {
		data, marshalErr := marshalChannelIndex(ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{
			"latest": {Release: "0.0.12", ManifestDigest: newerDigest},
		}})
		if marshalErr != nil {
			t.Fatalf("marshalChannelIndex: %v", marshalErr)
		}
		s.objects[name] = releaseObject{data: data, generation: s.nextGeneration}
		s.nextGeneration++
	}
	channelErr := store.PromoteChannel(context.Background(), "latest", "0.0.11", requestedDigest)
	if !errors.Is(channelErr, writeErr) {
		t.Fatalf("ambiguous promotion error=%v", channelErr)
	}
	index, fetchErr := store.FetchChannelIndex(context.Background())
	if fetchErr != nil || index.Channels["latest"] != (ChannelEntry{Release: "0.0.12", ManifestDigest: newerDigest}) {
		t.Fatalf("index=%+v err=%v", index, fetchErr)
	}
	if objects.replaceCalls != 1 {
		t.Fatalf("replace calls=%d want 1 without a rollback retry", objects.replaceCalls)
	}
}

func TestGCSStoreChannelPromotionPreservesPermissionError(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	initialDigest := "sha256:" + strings.Repeat("a", 64)
	if channelErr := store.PromoteChannel(context.Background(), "latest", "0.0.10", initialDigest); channelErr != nil {
		t.Fatalf("initial promotion: %v", channelErr)
	}
	permissionErr := errors.New("permission denied")
	objects.replaceErr = permissionErr
	channelErr := store.PromoteChannel(context.Background(), "latest", "0.0.11", "sha256:"+strings.Repeat("b", 64))
	if !errors.Is(channelErr, permissionErr) {
		t.Fatalf("promotion error=%v", channelErr)
	}
	if objects.replaceCalls != 1 {
		t.Fatalf("replace calls=%d want 1 without a CAS retry", objects.replaceCalls)
	}
}

func TestGCSReleasePreconditionErrorClassification(t *testing.T) {
	for _, testCase := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "google API precondition", err: &googleapi.Error{Code: 412}, want: true},
		{name: "wrapped google API precondition", err: &url.Error{Op: "PUT", Err: fmt.Errorf("upload: %w", &googleapi.Error{Code: 412})}, want: true},
		{name: "google API permission", err: &googleapi.Error{Code: 403}, want: false},
		{name: "transport", err: errors.New("connection reset"), want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isGCSReleasePreconditionFailure(testCase.err); got != testCase.want {
				t.Fatalf("isGCSReleasePreconditionFailure(%v)=%t want %t", testCase.err, got, testCase.want)
			}
		})
	}
}

func TestGCSStoreCoyoteLatestAndCancellation(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	advanced, promoteErr := store.PromoteCoyoteLatestIfNewer(context.Background(), "0.0.10", digest)
	if promoteErr != nil || !advanced {
		t.Fatalf("initial latest advanced=%t err=%v", advanced, promoteErr)
	}
	advanced, promoteErr = store.PromoteCoyoteLatestIfNewer(context.Background(), "0.0.9", digest)
	if promoteErr != nil || advanced {
		t.Fatalf("older latest advanced=%t err=%v", advanced, promoteErr)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, publishErr := store.PublishManifest(ctx, validManifest()); !errors.Is(publishErr, context.Canceled) {
		t.Fatalf("canceled publish error=%v", publishErr)
	}
	if channelErr := store.PromoteChannel(ctx, "stable", "0.0.10", digest); !errors.Is(channelErr, context.Canceled) {
		t.Fatalf("canceled channel promotion error=%v", channelErr)
	}
	if channelErr := store.PromoteChannel(context.Background(), "preview", "0.0.10", digest); channelErr == nil {
		t.Fatal("expected unsupported channel error")
	}
	if _, publishErr := store.PublishManifest(context.Background(), Manifest{}); publishErr == nil {
		t.Fatal("expected invalid manifest error")
	}
}

func TestGCSStoreConstructionAndLocationValidation(t *testing.T) {
	if _, storeErr := newGCSStore(nil, ""); storeErr == nil {
		t.Fatal("expected nil object store error")
	}
	for _, location := range []string{"gs://bucket", "gs://bucket/prefix/with-space"} {
		bucket, prefix, parseErr := parseGCSStoreLocation(location)
		if parseErr != nil || bucket != "bucket" {
			t.Fatalf("parse %q bucket=%q prefix=%q err=%v", location, bucket, prefix, parseErr)
		}
	}
	for _, location := range []string{"gs://", "gs://bucket:443", "gs://user@bucket", "gs://bucket/../escape", "gs://bucket/%2fescape", "gs://bucket?query=value"} {
		if _, _, parseErr := parseGCSStoreLocation(location); !errors.Is(parseErr, ErrInvalidLocation) {
			t.Fatalf("parse %q error=%v", location, parseErr)
		}
	}
	if _, storeErr := NewGCSStore(nil, "gs://bucket"); storeErr == nil {
		t.Fatal("expected nil client error")
	}
	client, clientErr := storage.NewClient(context.Background(), option.WithoutAuthentication())
	if clientErr != nil {
		t.Fatalf("storage.NewClient: %v", clientErr)
	}
	t.Cleanup(func() { _ = client.Close() })
	store, storeErr := NewGCSStore(client, "gs://bucket/prefix")
	if storeErr != nil || store.prefix != "prefix" {
		t.Fatalf("NewGCSStore store=%+v err=%v", store, storeErr)
	}
}

func TestGCSStorePublicationReturnsOriginalWriteErrorWhenUnverifiable(t *testing.T) {
	objects := newReleaseObjectStoreFake()
	writeErr := errors.New("permission denied")
	objects.createErr = writeErr
	store, storeErr := newGCSStore(objects, "")
	if storeErr != nil {
		t.Fatalf("newGCSStore: %v", storeErr)
	}
	if _, publishErr := store.PublishManifest(context.Background(), validManifest()); !errors.Is(publishErr, writeErr) {
		t.Fatalf("publish error=%v", publishErr)
	}
	var nilContext context.Context
	if normalized := releaseContext(nilContext); normalized == nil {
		t.Fatal("nil context was not normalized")
	}
}

type releaseObjectStoreFake struct {
	mu                   sync.Mutex
	objects              map[string]releaseObject
	nextGeneration       int64
	readErr              error
	createErr            error
	createAfterWriteErr  error
	replaceErr           error
	replaceAfterWriteErr error
	replaceAfterWrite    func(*releaseObjectStoreFake, string)
	replaceCalls         int
}

func newReleaseObjectStoreFake() *releaseObjectStoreFake {
	return &releaseObjectStoreFake{objects: map[string]releaseObject{}, nextGeneration: 1}
}

func (s *releaseObjectStoreFake) Read(ctx context.Context, name string, maxBytes int64) (releaseObject, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return releaseObject{}, contextErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readErr != nil {
		return releaseObject{}, s.readErr
	}
	object, found := s.objects[name]
	if !found {
		return releaseObject{}, storage.ErrObjectNotExist
	}
	if int64(len(object.data)) > maxBytes {
		return releaseObject{}, errors.New("object exceeds read limit")
	}
	object.data = append([]byte(nil), object.data...)
	return object, nil
}

func (s *releaseObjectStoreFake) Create(ctx context.Context, name string, data []byte) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	if _, found := s.objects[name]; found {
		return errGCSPrecondition
	}
	s.objects[name] = releaseObject{data: append([]byte(nil), data...), generation: s.nextGeneration}
	s.nextGeneration++
	if s.createAfterWriteErr != nil {
		return s.createAfterWriteErr
	}
	return nil
}

func (s *releaseObjectStoreFake) Replace(ctx context.Context, name string, generation int64, data []byte) error {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replaceCalls++
	if s.replaceErr != nil {
		return s.replaceErr
	}
	object, found := s.objects[name]
	if !found || object.generation != generation {
		return errGCSPrecondition
	}
	s.objects[name] = releaseObject{data: append([]byte(nil), data...), generation: s.nextGeneration}
	s.nextGeneration++
	if s.replaceAfterWrite != nil {
		s.replaceAfterWrite(s, name)
	}
	if s.replaceAfterWriteErr != nil {
		return s.replaceAfterWriteErr
	}
	return nil
}

func (s *releaseObjectStoreFake) put(name string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[name] = releaseObject{data: append([]byte(nil), data...), generation: s.nextGeneration}
	s.nextGeneration++
}

func (s *releaseObjectStoreFake) object(name string) (releaseObject, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	object, found := s.objects[name]
	return object, found
}

type synchronizedReplaceObjectStore struct {
	releaseObjectStore
	mu             sync.Mutex
	replaceCalls   int
	replaceReady   chan struct{}
	replaceProceed chan struct{}
}

func (s *synchronizedReplaceObjectStore) Replace(ctx context.Context, name string, generation int64, data []byte) error {
	s.mu.Lock()
	s.replaceCalls++
	if s.replaceCalls == 2 {
		close(s.replaceReady)
	}
	s.mu.Unlock()
	<-s.replaceProceed
	return s.releaseObjectStore.Replace(ctx, name, generation, data)
}
