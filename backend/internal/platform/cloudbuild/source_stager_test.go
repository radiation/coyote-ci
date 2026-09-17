package cloudbuild

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/radiation/coyote-ci/backend/internal/domain"
	"github.com/radiation/coyote-ci/backend/internal/service"
)

func TestCopySourceArchive(t *testing.T) {
	tests := []struct {
		name      string
		archive   io.Reader
		closeErr  error
		wantError error
		wantData  string
	}{
		{name: "success", archive: bytes.NewBufferString("archive"), wantData: "archive"},
		{name: "copy error", archive: errorReader{}, wantError: errSourceCopy},
		{name: "close error", archive: bytes.NewBufferString("archive"), closeErr: errSourceClose, wantError: errSourceClose, wantData: "archive"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			writer := &sourceWriterFake{closeErr: testCase.closeErr}
			err := copySourceArchive(writer, testCase.archive)
			if !errors.Is(err, testCase.wantError) || writer.data.String() != testCase.wantData {
				t.Fatalf("err=%v data=%q", err, writer.data.String())
			}
		})
	}
}

func TestSourceStagerRejectsMissingExecutionIDOrArchive(t *testing.T) {
	stager := &SourceStager{}
	if _, err := stager.Stage(context.Background(), "", bytes.NewReader(nil), "", nil); err == nil {
		t.Fatal("expected missing execution job ID error")
	}
	if _, err := stager.Stage(context.Background(), "job-1", nil, "", nil); err == nil {
		t.Fatal("expected missing archive error")
	}
}

func TestCopySourceArchiveWithArtifactsAddsResolvedInputsToDockerContext(t *testing.T) {
	sourceArchive := gzipArchive(t, map[string]string{
		"backend/Dockerfile": "FROM scratch\n",
		"README.md":          "source\n",
	})
	server := []byte("server binary")
	worker := []byte("worker binary")
	artifacts := []service.ImageBuildContextArtifact{
		imageBuildContextArtifact("coyote-server", "dist/coyote-server", server),
		imageBuildContextArtifact("coyote-worker", "dist/coyote-worker", worker),
	}
	writer := &sourceWriterFake{}

	if err := copySourceArchiveWithArtifacts(writer, bytes.NewReader(sourceArchive), "backend", artifacts); err != nil {
		t.Fatalf("stage source archive: %v", err)
	}
	entries := readGzipArchive(t, writer.data.Bytes())
	for name, want := range map[string][]byte{
		"backend/dist/coyote-server": server,
		"backend/dist/coyote-worker": worker,
	} {
		got, ok := entries[name]
		if !ok {
			t.Fatalf("missing injected artifact %q", name)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("artifact %q = %q, want %q", name, got, want)
		}
		checksum := sha256.Sum256(got)
		wantChecksum := sha256.Sum256(want)
		if checksum != wantChecksum {
			t.Fatalf("artifact %q checksum = %s, want %s", name, hex.EncodeToString(checksum[:]), hex.EncodeToString(wantChecksum[:]))
		}
	}
}

func TestCopySourceArchiveWithArtifactsRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name      string
		archive   []byte
		artifacts []service.ImageBuildContextArtifact
		wantError string
	}{
		{
			name:      "invalid source archive",
			archive:   []byte("not a gzip archive"),
			artifacts: []service.ImageBuildContextArtifact{imageBuildContextArtifact("server", "dist/server", []byte("server"))},
			wantError: "opening source archive",
		},
		{
			name:    "duplicate destination",
			archive: gzipArchive(t, map[string]string{"backend/Dockerfile": "FROM scratch\n"}),
			artifacts: []service.ImageBuildContextArtifact{
				imageBuildContextArtifact("server", "dist/server", []byte("server")),
				imageBuildContextArtifact("worker", "dist/server", []byte("worker")),
			},
			wantError: "duplicate artifact destination",
		},
		{
			name:      "destination already exists in source",
			archive:   gzipArchive(t, map[string]string{"backend/dist/server": "source"}),
			artifacts: []service.ImageBuildContextArtifact{imageBuildContextArtifact("server", "dist/server", []byte("server"))},
			wantError: "already exists in source context",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			writer := &sourceWriterFake{}
			err := copySourceArchiveWithArtifacts(writer, bytes.NewReader(testCase.archive), "backend", testCase.artifacts)
			if err == nil || !strings.Contains(err.Error(), testCase.wantError) {
				t.Fatalf("error=%v, want %q", err, testCase.wantError)
			}
		})
	}
}

func imageBuildContextArtifact(name, destination string, content []byte) service.ImageBuildContextArtifact {
	checksum := sha256.Sum256(content)
	return service.ImageBuildContextArtifact{
		Artifact: domain.ImageBuildArtifact{Name: name, Destination: destination, SizeBytes: int64(len(content)), ChecksumSHA256: hex.EncodeToString(checksum[:])},
		Source:   io.NopCloser(bytes.NewReader(content)),
	}
}

func gzipArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range entries {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func readGzipArchive(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if closeErr := gzipReader.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}()
	entries := map[string][]byte{}
	tarReader := tar.NewReader(gzipReader)
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			return entries
		}
		if nextErr != nil {
			t.Fatal(nextErr)
		}
		content, readErr := io.ReadAll(tarReader)
		if readErr != nil {
			t.Fatal(readErr)
		}
		entries[header.Name] = content
	}
}

var (
	errSourceCopy  = errors.New("copy failed")
	errSourceClose = errors.New("close failed")
)

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errSourceCopy }

type sourceWriterFake struct {
	data     bytes.Buffer
	closeErr error
}

func (f *sourceWriterFake) Write(value []byte) (int, error) { return f.data.Write(value) }
func (f *sourceWriterFake) Close() error                    { return f.closeErr }
