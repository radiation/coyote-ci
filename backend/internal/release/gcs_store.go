package release

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"cloud.google.com/go/storage"
	"google.golang.org/api/googleapi"
)

const (
	maxReleaseDocumentBytes = 1 << 20
	channelCASAttempts      = 8
)

var errGCSPrecondition = errors.New("gcs generation precondition failed")

type releaseObject struct {
	data       []byte
	generation int64
}

// releaseObjectStore is the provider boundary used by GCSStore. It keeps GCS
// generation and writer mechanics out of the provider-neutral Store contract.
type releaseObjectStore interface {
	Read(context.Context, string, int64) (releaseObject, error)
	Create(context.Context, string, []byte) error
	Replace(context.Context, string, int64, []byte) error
}

// GCSStore persists release documents in a GCS bucket using object generation
// preconditions. The caller owns the supplied client lifecycle.
type GCSStore struct {
	objects releaseObjectStore
	prefix  string
}

// NewGCSStore builds a release store for a validated gs:// location. The
// supplied storage client normally uses Application Default Credentials.
func NewGCSStore(client *storage.Client, location string) (*GCSStore, error) {
	if client == nil {
		return nil, errors.New("gcs release store client is required")
	}
	bucket, prefix, locationErr := parseGCSStoreLocation(location)
	if locationErr != nil {
		return nil, locationErr
	}
	return newGCSStore(&gcsReleaseObjectStore{bucket: client.Bucket(bucket)}, prefix)
}

func newGCSStore(objects releaseObjectStore, prefix string) (*GCSStore, error) {
	if objects == nil {
		return nil, errors.New("gcs release object store is required")
	}
	return &GCSStore{objects: objects, prefix: strings.Trim(prefix, "/")}, nil
}

func (s *GCSStore) FetchManifest(ctx context.Context, version string) (Document, error) {
	object, readErr := s.read(ctx, s.manifestObjectName(version))
	if readErr != nil {
		return Document{}, readErr
	}
	return Document{Data: object.data, Digest: Digest(object.data)}, nil
}

func (s *GCSStore) FetchChannelIndex(ctx context.Context) (ChannelIndex, error) {
	object, readErr := s.read(ctx, s.channelsObjectName())
	if readErr != nil {
		return ChannelIndex{}, readErr
	}
	return decodeChannelIndex(object.data)
}

func (s *GCSStore) PublishManifest(ctx context.Context, manifest Manifest) (string, error) {
	ctx = releaseContext(ctx)
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if validateErr := manifest.Validate(); validateErr != nil {
		return "", validateErr
	}
	data, marshalErr := marshalManifest(manifest)
	if marshalErr != nil {
		return "", marshalErr
	}
	return s.publishManifestData(ctx, manifest.Release, data)
}

func (s *GCSStore) ImportManifest(ctx context.Context, data []byte) (Manifest, string, error) {
	manifest, parseErr := ParseManifest(data)
	if parseErr != nil {
		return Manifest{}, "", parseErr
	}
	digest, publishErr := s.publishManifestData(ctx, manifest.Release, data)
	if publishErr != nil {
		return Manifest{}, "", publishErr
	}
	return manifest, digest, nil
}

func (s *GCSStore) publishManifestData(ctx context.Context, releaseVersion string, data []byte) (string, error) {
	ctx = releaseContext(ctx)
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if len(data) > maxReleaseDocumentBytes {
		return "", fmt.Errorf("release manifest exceeds %d byte limit", maxReleaseDocumentBytes)
	}
	objectName := s.manifestObjectName(releaseVersion)
	createErr := s.objects.Create(ctx, objectName, data)
	if createErr == nil {
		return Digest(data), nil
	}
	return s.reconcileManifestPublication(ctx, objectName, data, createErr)
}

func (s *GCSStore) PromoteChannel(ctx context.Context, channel, releaseVersion, manifestDigest string) (ChannelPromotionResult, error) {
	if channel != "latest" && channel != "stable" {
		return ChannelPromotionResult{}, fmt.Errorf("unsupported release channel %q", channel)
	}
	if !semVerPattern.MatchString(releaseVersion) || !digestPattern.MatchString(manifestDigest) {
		return ChannelPromotionResult{}, errors.New("channel promotion requires an exact release and manifest digest")
	}
	requested := ChannelEntry{Release: releaseVersion, ManifestDigest: manifestDigest}
	ctx = releaseContext(ctx)
	for attempt := 0; attempt < channelCASAttempts; attempt++ {
		if contextErr := ctx.Err(); contextErr != nil {
			return ChannelPromotionResult{}, contextErr
		}
		index, generation, readErr := s.readChannels(ctx)
		if readErr != nil {
			return ChannelPromotionResult{}, readErr
		}
		result := ChannelPromotionResult{Current: requested, PreviousKnown: true, ChangedKnown: true}
		if previous, found := index.Channels[channel]; found {
			result.Previous = &previous
			if previous == requested {
				return result, nil
			}
		}
		index.Channels[channel] = requested
		data, marshalErr := marshalChannelIndex(index)
		if marshalErr != nil {
			return ChannelPromotionResult{}, marshalErr
		}
		var writeErr error
		if generation == 0 {
			writeErr = s.objects.Create(ctx, s.channelsObjectName(), data)
		} else {
			writeErr = s.objects.Replace(ctx, s.channelsObjectName(), generation, data)
		}
		if writeErr == nil {
			result.Changed = true
			return result, nil
		}
		if errors.Is(writeErr, errGCSPrecondition) {
			continue
		}
		index, _, readErr = s.readChannels(ctx)
		if readErr != nil {
			return ChannelPromotionResult{}, reconciliationError("promote channel", writeErr, readErr)
		}
		if channelPromotionApplied(channel, requested)(index) {
			return ChannelPromotionResult{Current: requested}, nil
		}
		return ChannelPromotionResult{}, writeErr
	}
	return ChannelPromotionResult{}, ErrChannelPromotionBusy
}

func (s *GCSStore) PromoteCoyoteLatestIfNewer(ctx context.Context, releaseVersion, manifestDigest string) (bool, error) {
	if !semVerPattern.MatchString(releaseVersion) || !digestPattern.MatchString(manifestDigest) {
		return false, errors.New("channel promotion requires an exact release and manifest digest")
	}
	requested := ChannelEntry{Release: releaseVersion, ManifestDigest: manifestDigest}
	return s.promoteChannels(ctx, func(index *ChannelIndex) (bool, error) {
		if current, found := index.Channels["latest"]; found {
			currentNumber, currentOK := coyoteBuildNumber(current.Release)
			candidateNumber, candidateOK := coyoteBuildNumber(releaseVersion)
			if !currentOK || !candidateOK {
				return false, errors.New("automatic Coyote latest requires 0.0.<build_number> releases")
			}
			if candidateNumber < currentNumber {
				return false, nil
			}
		}
		index.Channels["latest"] = requested
		return true, nil
	}, channelPromotionApplied("latest", requested))
}

func (s *GCSStore) reconcileManifestPublication(ctx context.Context, objectName string, data []byte, createErr error) (string, error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	existing, readErr := s.read(ctx, objectName)
	if readErr != nil {
		return "", reconciliationError("publish manifest", createErr, readErr)
	}
	if !bytes.Equal(existing.data, data) {
		return "", ErrManifestConflict
	}
	return Digest(existing.data), nil
}

func (s *GCSStore) promoteChannels(ctx context.Context, mutate func(*ChannelIndex) (bool, error), applied func(ChannelIndex) bool) (bool, error) {
	ctx = releaseContext(ctx)
	for attempt := 0; attempt < channelCASAttempts; attempt++ {
		if contextErr := ctx.Err(); contextErr != nil {
			return false, contextErr
		}
		index, generation, readErr := s.readChannels(ctx)
		if readErr != nil {
			return false, readErr
		}
		advanced, mutateErr := mutate(&index)
		if mutateErr != nil || !advanced {
			return advanced, mutateErr
		}
		data, marshalErr := marshalChannelIndex(index)
		if marshalErr != nil {
			return false, marshalErr
		}
		var writeErr error
		if generation == 0 {
			writeErr = s.objects.Create(ctx, s.channelsObjectName(), data)
		} else {
			writeErr = s.objects.Replace(ctx, s.channelsObjectName(), generation, data)
		}
		if writeErr == nil {
			return true, nil
		}
		if errors.Is(writeErr, errGCSPrecondition) {
			continue
		}
		return s.reconcileChannelPromotion(ctx, applied, writeErr)
	}
	return false, ErrChannelPromotionBusy
}

func (s *GCSStore) reconcileChannelPromotion(ctx context.Context, applied func(ChannelIndex) bool, writeErr error) (bool, error) {
	index, _, readErr := s.readChannels(ctx)
	if readErr != nil {
		return false, reconciliationError("promote channel", writeErr, readErr)
	}
	if applied(index) {
		return true, nil
	}
	return false, writeErr
}

func channelPromotionApplied(channel string, requested ChannelEntry) func(ChannelIndex) bool {
	return func(index ChannelIndex) bool {
		return index.Channels[channel] == requested
	}
}

func reconciliationError(operation string, writeErr, readErr error) error {
	return fmt.Errorf("%s write failed: %w; reconciliation read failed: %w", operation, writeErr, readErr)
}

func (s *GCSStore) readChannels(ctx context.Context) (ChannelIndex, int64, error) {
	object, readErr := s.read(ctx, s.channelsObjectName())
	if errors.Is(readErr, ErrNotFound) {
		return ChannelIndex{SchemaVersion: SchemaVersion, Channels: map[string]ChannelEntry{}}, 0, nil
	}
	if readErr != nil {
		return ChannelIndex{}, 0, readErr
	}
	index, decodeErr := decodeChannelIndex(object.data)
	if decodeErr != nil {
		return ChannelIndex{}, 0, decodeErr
	}
	if validateErr := index.Validate(); validateErr != nil {
		return ChannelIndex{}, 0, validateErr
	}
	return index, object.generation, nil
}

func (s *GCSStore) read(ctx context.Context, objectName string) (releaseObject, error) {
	ctx = releaseContext(ctx)
	if contextErr := ctx.Err(); contextErr != nil {
		return releaseObject{}, contextErr
	}
	object, readErr := s.objects.Read(ctx, objectName, maxReleaseDocumentBytes)
	if errors.Is(readErr, storage.ErrObjectNotExist) {
		return releaseObject{}, fmt.Errorf("%w: %s", ErrNotFound, objectName)
	}
	return object, readErr
}

func (s *GCSStore) manifestObjectName(version string) string {
	return s.objectName(path.Join("releases", version+".json"))
}

func (s *GCSStore) channelsObjectName() string {
	return s.objectName("channels.json")
}

func (s *GCSStore) objectName(name string) string {
	if s.prefix == "" {
		return name
	}
	return path.Join(s.prefix, name)
}

func releaseContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func marshalManifest(manifest Manifest) ([]byte, error) {
	data, marshalErr := json.MarshalIndent(manifest, "", "  ")
	if marshalErr != nil {
		return nil, marshalErr
	}
	return append(data, '\n'), nil
}

func marshalChannelIndex(index ChannelIndex) ([]byte, error) {
	data, marshalErr := json.MarshalIndent(index, "", "  ")
	if marshalErr != nil {
		return nil, marshalErr
	}
	return append(data, '\n'), nil
}

func decodeChannelIndex(data []byte) (ChannelIndex, error) {
	var index ChannelIndex
	if decodeErr := json.Unmarshal(data, &index); decodeErr != nil {
		return ChannelIndex{}, fmt.Errorf("decode channel index: %w", decodeErr)
	}
	return index, nil
}

type gcsReleaseObjectStore struct {
	bucket *storage.BucketHandle
}

func (s *gcsReleaseObjectStore) Read(ctx context.Context, objectName string, maxBytes int64) (releaseObject, error) {
	reader, readerErr := s.bucket.Object(objectName).NewReader(ctx)
	if readerErr != nil {
		return releaseObject{}, readerErr
	}
	defer func() { _ = reader.Close() }()
	data, readErr := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if readErr != nil {
		return releaseObject{}, readErr
	}
	if int64(len(data)) > maxBytes {
		return releaseObject{}, fmt.Errorf("release object %q exceeds %d byte limit", objectName, maxBytes)
	}
	if closeErr := reader.Close(); closeErr != nil {
		return releaseObject{}, closeErr
	}
	return releaseObject{data: data, generation: reader.Attrs.Generation}, nil
}

func (s *gcsReleaseObjectStore) Create(ctx context.Context, objectName string, data []byte) error {
	return s.write(ctx, s.bucket.Object(objectName).If(storage.Conditions{DoesNotExist: true}), data)
}

func (s *gcsReleaseObjectStore) Replace(ctx context.Context, objectName string, generation int64, data []byte) error {
	return s.write(ctx, s.bucket.Object(objectName).If(storage.Conditions{GenerationMatch: generation}), data)
}

func (s *gcsReleaseObjectStore) write(ctx context.Context, object *storage.ObjectHandle, data []byte) error {
	writer := object.NewWriter(ctx)
	if _, copyErr := io.Copy(writer, bytes.NewReader(data)); copyErr != nil {
		_ = writer.Close()
		return copyErr
	}
	if closeErr := writer.Close(); closeErr != nil {
		if isGCSReleasePreconditionFailure(closeErr) {
			return errGCSPrecondition
		}
		return closeErr
	}
	return nil
}

func isGCSReleasePreconditionFailure(err error) bool {
	var apiErr *googleapi.Error
	return errors.As(err, &apiErr) && apiErr.Code == 412
}
