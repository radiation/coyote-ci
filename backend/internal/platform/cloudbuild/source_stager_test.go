package cloudbuild

import (
	"bytes"
	"errors"
	"io"
	"testing"
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
