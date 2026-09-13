package cloudbuild

import (
	"context"
	"fmt"
	"io"
	"strings"

	"cloud.google.com/go/storage"

	"github.com/radiation/coyote-ci/backend/internal/domain"
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

func (s *SourceStager) Stage(ctx context.Context, executionJobID string, archive io.Reader) (domain.ImageBuildSource, error) {
	if strings.TrimSpace(executionJobID) == "" || archive == nil {
		return domain.ImageBuildSource{}, fmt.Errorf("execution job id and archive are required")
	}
	object := strings.Trim(s.prefix+"/"+executionJobID+".tar.gz", "/")
	writerContext, cancelWriter := context.WithCancel(ctx)
	defer cancelWriter()
	writer := s.bucket.Object(object).If(storage.Conditions{DoesNotExist: true}).NewWriter(writerContext)
	writer.ContentType = "application/gzip"
	if copyErr := copySourceArchive(writer, archive); copyErr != nil {
		cancelWriter()
		return domain.ImageBuildSource{}, copyErr
	}
	attrs, err := s.bucket.Object(object).Attrs(ctx)
	if err != nil {
		return domain.ImageBuildSource{}, err
	}
	return domain.ImageBuildSource{Bucket: s.bucket.BucketName(), Object: object, Generation: fmt.Sprintf("%d", attrs.Generation)}, nil
}

func copySourceArchive(writer io.WriteCloser, archive io.Reader) error {
	if _, err := io.Copy(writer, archive); err != nil {
		return err
	}
	return writer.Close()
}
