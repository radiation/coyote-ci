package cloudbuild

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

type SourceStager struct {
	bucket *storage.BucketHandle
	prefix string
}

func NewSourceStager(client *storage.Client, bucket string, prefix string) (*SourceStager, error) {
	if client == nil || strings.TrimSpace(bucket) == "" {
		return nil, fmt.Errorf("cloud build source stager requires storage client and bucket")
	}
	return &SourceStager{bucket: client.Bucket(strings.TrimSpace(bucket)), prefix: strings.Trim(strings.TrimSpace(prefix), "/")}, nil
}

func (s *SourceStager) Stage(ctx context.Context, executionJobID string, archive io.Reader, contextPath string, artifacts []service.ImageBuildContextArtifact) (domain.ImageBuildSource, error) {
	if strings.TrimSpace(executionJobID) == "" || archive == nil {
		return domain.ImageBuildSource{}, fmt.Errorf("execution job id and archive are required")
	}
	object := strings.Trim(s.prefix+"/"+executionJobID+".tar.gz", "/")
	writerContext, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()
	writer := s.bucket.Object(object).If(storage.Conditions{DoesNotExist: true}).NewWriter(writerContext)
	writer.ContentType = "application/gzip"
	var copyErr error
	if len(artifacts) == 0 {
		copyErr = copySourceArchive(writer, archive)
	} else {
		copyErr = copySourceArchiveWithArtifacts(writer, archive, contextPath, artifacts)
	}
	if copyErr != nil {
		cancelWriter()
		return domain.ImageBuildSource{}, copyErr
	}
	attrs, err := s.bucket.Object(object).Attrs(ctx)
	if err != nil {
		return domain.ImageBuildSource{}, err
	}
	return domain.ImageBuildSource{Bucket: s.bucket.BucketName(), Object: object, Generation: fmt.Sprintf("%d", attrs.Generation)}, nil
}

func copySourceArchiveWithArtifacts(writer io.WriteCloser, archive io.Reader, contextPath string, artifacts []service.ImageBuildContextArtifact) (err error) {
	archiveReader, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("opening source archive: %w", err)
	}
	defer func() {
		if closeErr := archiveReader.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	archiveWriter := gzip.NewWriter(writer)
	tarReader, tarWriter := tar.NewReader(archiveReader), tar.NewWriter(archiveWriter)
	targets := make(map[string]service.ImageBuildContextArtifact, len(artifacts))
	for _, artifact := range artifacts {
		target := path.Join(strings.Trim(contextPath, "/"), strings.Trim(artifact.Artifact.Destination, "/"))
		if target == "." || strings.HasPrefix(target, "../") {
			return fmt.Errorf("invalid artifact destination %q", artifact.Artifact.Destination)
		}
		if _, exists := targets[target]; exists {
			return fmt.Errorf("duplicate artifact destination %q", artifact.Artifact.Destination)
		}
		targets[target] = artifact
	}
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return fmt.Errorf("reading source archive: %w", nextErr)
		}
		name := strings.TrimSuffix(header.Name, "/")
		if _, conflict := targets[name]; conflict {
			return fmt.Errorf("artifact destination %q already exists in source context", name)
		}
		if writeErr := tarWriter.WriteHeader(header); writeErr != nil {
			return writeErr
		}
		if _, copyErr := io.Copy(tarWriter, tarReader); copyErr != nil {
			return copyErr
		}
	}
	for target, artifact := range targets {
		if err := tarWriter.WriteHeader(&tar.Header{Name: target, Mode: 0755, Size: artifact.Artifact.SizeBytes, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		if _, err := io.Copy(tarWriter, artifact.Source); err != nil {
			return fmt.Errorf("copying artifact %q: %w", artifact.Artifact.Name, err)
		}
		if err := artifact.Source.Close(); err != nil {
			return err
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := archiveWriter.Close(); err != nil {
		return err
	}
	return writer.Close()
}

func copySourceArchive(writer io.WriteCloser, archive io.Reader) error {
	if _, err := io.Copy(writer, archive); err != nil {
		return err
	}
	return writer.Close()
}
