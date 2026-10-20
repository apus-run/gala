package s3

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
)

func TestClassifyMapsServiceErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want error
	}{
		{"missing key", &apiErr{code: "NoSuchKey", status: http.StatusNotFound, msg: "no"}, fileserver.ErrNotFound},
		{"not found status", &apiErr{code: "X", status: http.StatusNotFound, msg: "no"}, fileserver.ErrNotFound},
		{"denied", &apiErr{code: "AccessDenied", status: http.StatusForbidden, msg: "no"}, fileserver.ErrPermission},
		{"forbidden status", &apiErr{code: "X", status: http.StatusForbidden, msg: "no"}, fileserver.ErrPermission},
		{"precondition", &apiErr{code: "PreconditionFailed", status: http.StatusPreconditionFailed, msg: "no"}, fileserver.ErrConflict},
		{"conflict status", &apiErr{code: "X", status: http.StatusConflict, msg: "no"}, fileserver.ErrConflict},
		{"too large", &apiErr{code: "EntityTooLarge", status: http.StatusRequestEntityTooLarge, msg: "no"}, fileserver.ErrTooLarge},
		{"not implemented", &apiErr{code: "NotImplemented", status: http.StatusNotImplemented, msg: "no"}, fileserver.ErrUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classify(tc.err)
			if !errors.Is(got, tc.want) {
				t.Fatalf("classify(%v) = %v, want %v", tc.err, got, tc.want)
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("classify(%v) dropped the cause", tc.err)
			}
		})
	}
}

func TestClassifyKeepsUnmappedErrors(t *testing.T) {
	noBucket := &apiErr{code: "NoSuchBucket", status: http.StatusNotFound, msg: "no"}
	if got := classify(noBucket); !errors.Is(got, noBucket) {
		t.Fatalf("classify(NoSuchBucket) = %v, want the original error", got)
	}
	other := &apiErr{code: "SlowDown", status: http.StatusServiceUnavailable, msg: "no"}
	if got := classify(other); !errors.Is(got, other) {
		t.Fatalf("classify(SlowDown) = %v, want the original error", got)
	}
}

// TestPutObjectNotImplementedIsUnsupported pins the outcome of a write the
// service did not implement (501): the request was not processed, so the
// result is known, not indeterminate.
func TestPutObjectNotImplementedIsUnsupported(t *testing.T) {
	api := newMemAPI()
	api.errFor = map[string]error{
		"PutObject": &apiErr{code: "NotImplemented", status: http.StatusNotImplemented, msg: "no if-none-match"},
	}
	s := newTestStore(t, api, Config{})
	err := s.Upload(t.Context(), "k", bytes.NewReader([]byte("x")), fileserver.WithIfNotExists())
	if !errors.Is(err, fileserver.ErrUnsupported) {
		t.Fatalf("Upload error = %v, want ErrUnsupported", err)
	}
	if errors.Is(err, fileserver.ErrIndeterminate) {
		t.Fatalf("Upload error = %v, want a definite outcome", err)
	}
	if _, statErr := s.Stat(t.Context(), "k"); !errors.Is(statErr, fileserver.ErrNotFound) {
		t.Fatalf("the object became visible: %v", statErr)
	}
}

// TestCompleteWithoutUploadIsIndeterminate covers the case where the service
// forgot the multipart upload before the completion: the object may or may
// not have been written, so the outcome is unknown.
func TestCompleteWithoutUploadIsIndeterminate(t *testing.T) {
	api := newMemAPI()
	api.errFor = map[string]error{"CompleteMultipartUpload": errNoSuchUpload()}
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+1)
	err := s.Upload(t.Context(), "k", bytes.NewReader(data))
	if !errors.Is(err, fileserver.ErrIndeterminate) {
		t.Fatalf("Upload error = %v, want ErrIndeterminate", err)
	}
	if api.aborts == 0 {
		t.Fatal("the multipart upload was not aborted")
	}
}

// TestAbortFailureIsAttachedToTheError checks that a failing cleanup is
// reported next to the original failure instead of being swallowed.
func TestAbortFailureIsAttachedToTheError(t *testing.T) {
	api := newMemAPI()
	api.errFor = map[string]error{
		"CompleteMultipartUpload": &apiErr{code: "InternalError", status: http.StatusInternalServerError, msg: "boom"},
		"AbortMultipartUpload":    &apiErr{code: "AccessDenied", status: http.StatusForbidden, msg: "denied"},
	}
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+1)
	err := s.Upload(t.Context(), "k", bytes.NewReader(data))
	if !errors.Is(err, fileserver.ErrIndeterminate) {
		t.Fatalf("Upload error = %v, want ErrIndeterminate", err)
	}
	if !errors.Is(err, fileserver.ErrPermission) {
		t.Fatalf("Upload error = %v, want the abort failure attached", err)
	}
	if !strings.Contains(err.Error(), "abort multipart upload") {
		t.Fatalf("Upload error = %q, want it to mention the abort", err)
	}
}

// TestAbortTreatsAMissingUploadAsSuccess checks the cleanup path: the upload is
// already gone, so there is nothing left to abort.
func TestAbortTreatsAMissingUploadAsSuccess(t *testing.T) {
	api := newMemAPI()
	api.errFor = map[string]error{
		"CompleteMultipartUpload": &apiErr{code: "InternalError", status: http.StatusInternalServerError, msg: "boom"},
		"AbortMultipartUpload":    errNoSuchUpload(),
	}
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+1)
	err := s.Upload(t.Context(), "k", bytes.NewReader(data))
	if !errors.Is(err, fileserver.ErrIndeterminate) {
		t.Fatalf("Upload error = %v, want ErrIndeterminate", err)
	}
	if errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("Upload error = %v, want the missing upload to be ignored", err)
	}
}

func TestRemoveReportsPermissionErrors(t *testing.T) {
	api := newMemAPI()
	api.errFor = map[string]error{
		"DeleteObject": &apiErr{code: "AccessDenied", status: http.StatusForbidden, msg: "denied"},
	}
	s := newTestStore(t, api, Config{})
	err := s.Remove(t.Context(), "k")
	if !errors.Is(err, fileserver.ErrPermission) {
		t.Fatalf("Remove error = %v, want ErrPermission", err)
	}
}

// TestMultipartUploadPersistsContentTypeAndMetadata covers the multipart
// path, where the content type and the metadata are written when the upload
// is created rather than with the object.
func TestMultipartUploadPersistsContentTypeAndMetadata(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+8)
	err := s.Upload(t.Context(), "k", bytes.NewReader(data),
		fileserver.WithContentType("application/octet-stream"),
		fileserver.WithMetadata(map[string]string{"Owner": "team"}))
	if err != nil {
		t.Fatal(err)
	}
	if api.creates == 0 {
		t.Fatal("the upload did not use the multipart path")
	}
	info, err := s.Stat(t.Context(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if info.FContentType != "application/octet-stream" {
		t.Fatalf("ContentType = %q, want application/octet-stream", info.FContentType)
	}
	if info.FMetadata["owner"] != "team" {
		t.Fatalf("Metadata = %v, want owner=team", info.FMetadata)
	}
}

// TestUploadRejectsInvalidContentTypeAndMetadata checks that values the
// service cannot store are rejected before anything is sent.
func TestUploadRejectsInvalidContentTypeAndMetadata(t *testing.T) {
	cases := []struct {
		name string
		opts []fileserver.UploadOpt
	}{
		{"control character in content type", []fileserver.UploadOpt{fileserver.WithContentType("text/plain\r\nX: 1")}},
		{"non-ascii content type", []fileserver.UploadOpt{fileserver.WithContentType("文本/plain")}},
		{"metadata key with a space", []fileserver.UploadOpt{fileserver.WithMetadata(map[string]string{"owner name": "team"})}},
		{"metadata value with a control character", []fileserver.UploadOpt{fileserver.WithMetadata(map[string]string{"owner": "a\nb"})}},
		{"metadata that is too large", []fileserver.UploadOpt{fileserver.WithMetadata(map[string]string{"owner": strings.Repeat("x", maxMetadataBytes)})}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newMemAPI()
			s := newTestStore(t, api, Config{})
			err := s.Upload(t.Context(), "k", bytes.NewReader([]byte("x")), tc.opts...)
			if !errors.Is(err, fileserver.ErrInvalidArgument) {
				t.Fatalf("Upload error = %v, want ErrInvalidArgument", err)
			}
			if api.puts != 0 || api.creates != 0 {
				t.Fatalf("the object reached the service: puts=%d creates=%d", api.puts, api.creates)
			}
		})
	}
}

// TestRangedReadRequiresETag checks the version guard of a ranged read: a
// service that does not return an ETag cannot prove that the size from HEAD
// and the bytes from GET belong to the same version.
func TestRangedReadRequiresETag(t *testing.T) {
	api := newMemAPI()
	s, err := New(&emptyETagAPI{api}, Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), "k", bytes.NewReader([]byte("abcdef"))); err != nil {
		t.Fatal(err)
	}
	_, err = s.Read(t.Context(), "k", fileserver.WithRange(1, 2))
	if !errors.Is(err, fileserver.ErrUnsupported) {
		t.Fatalf("ranged Read error = %v, want ErrUnsupported", err)
	}
	rc, err := s.Read(t.Context(), "k")
	if err != nil {
		t.Fatalf("whole-object Read: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestSegmentedDownloadRejectsShortResponseBody covers the per-span length
// check: a body that is shorter than the requested range with an unchanged
// ETag is only visible by comparing the copied byte count.
func TestSegmentedDownloadRejectsShortResponseBody(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{Concurrency: 2})
	payload := bytes.Repeat([]byte{'A'}, int(s.partSize)+10)
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.shortBody = 1
	api.mu.Unlock()

	var buf fileservertest.BufferAt
	err := s.Download(t.Context(), "obj", &buf)
	if !errors.Is(err, fileserver.ErrConflict) {
		t.Fatalf("Download error = %v, want ErrConflict", err)
	}
}

// TestUploadHonorsContextDuringComplete checks that a canceled context stops
// the upload before it is committed.
func TestUploadHonorsContextDuringComplete(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := s.Upload(ctx, "k", bytes.NewReader(data))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Upload error = %v, want context.Canceled", err)
	}
}

// The real SDK wraps transport failures in an HTTP error with status zero.
func TestWriteStatusZeroIsIndeterminate(t *testing.T) {
	cause := &apiErr{code: "", status: 0, msg: "response lost"}
	got := classifyWrite(cause)
	if !errors.Is(got, fileserver.ErrIndeterminate) || !errors.Is(got, cause) {
		t.Fatalf("classifyWrite(status=0) = %v, want indeterminate with cause", got)
	}
}
