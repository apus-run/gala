package s3

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
	"github.com/apus-run/gala/components/fileserver/objectstore"
)

func newTestStore(t *testing.T, api *memAPI, cfg Config) *Store {
	t.Helper()
	if cfg.Bucket == "" {
		cfg.Bucket = "bucket"
	}
	if cfg.PartSize == 0 {
		cfg.PartSize = 5 << 20
	}
	s, err := New(api, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

func TestStorageSuite(t *testing.T) {
	fileservertest.RunStorageSuite(t, func(t *testing.T) fileserver.Storage {
		return newTestStore(t, newMemAPI(), Config{Presigner: newMemAPI()})
	}, fileservertest.Capabilities{
		ContentType: true,
		Metadata:    true,
		IfNotExists: true,
		ModTime:     true,
		ETag:        true,
	})
}

func TestNewRejectsBadConfig(t *testing.T) {
	api := newMemAPI()
	cases := []Config{
		{},
		{Bucket: "has/slash"},
		{Bucket: "b", Prefix: "/../x"},
		{Bucket: "b", PartSize: MinPartSize - 1},
		{Bucket: "b", PartSize: MaxPartSize + 1},
		{Bucket: "b", Concurrency: 33},
		{Bucket: "b", MaxObjectSize: -1},
	}
	for i, cfg := range cases {
		if _, err := New(api, cfg); !errors.Is(err, fileserver.ErrInvalidArgument) {
			t.Errorf("config %d: New error = %v, want ErrInvalidArgument", i, err)
		}
	}
	if _, err := New(nil, Config{Bucket: "b"}); !errors.Is(err, fileserver.ErrInvalidArgument) {
		t.Fatalf("nil API error = %v, want ErrInvalidArgument", err)
	}
	s, err := New(api, Config{Bucket: "b", Concurrency: 0, PartSize: 0})
	if err != nil {
		t.Fatal(err)
	}
	if s.partSize != DefaultPartSize || s.concurrency != DefaultConcurrency {
		t.Fatalf("defaults = part %d concurrency %d", s.partSize, s.concurrency)
	}
}

func TestUploadUsesChecksumAndAbortsShortMultipart(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{})
	part := int(s.partSize)
	data := bytes.Repeat([]byte("a"), part+10)
	err := s.Upload(t.Context(), "big", bytes.NewReader(data[:part+3]), fileserver.WithSize(int64(len(data))))
	if !errors.Is(err, fileserver.ErrSizeMismatch) {
		t.Fatalf("Upload error = %v, want ErrSizeMismatch", err)
	}
	if _, statErr := s.Stat(t.Context(), "big"); !errors.Is(statErr, fileserver.ErrNotFound) {
		t.Fatalf("Stat after rejected upload = %v, want ErrNotFound", statErr)
	}
	if api.uploadCount() != 0 {
		t.Fatalf("multipart uploads left = %d, want 0", api.uploadCount())
	}
	if api.aborts == 0 || api.creates == 0 {
		t.Fatalf("creates = %d, aborts = %d, want both positive", api.creates, api.aborts)
	}
	if api.lastAlg != checksumAlgorithm {
		t.Fatalf("checksum algorithm = %q", api.lastAlg)
	}

	if err := s.Upload(t.Context(), "small", bytes.NewReader([]byte("ok"))); err != nil {
		t.Fatal(err)
	}
	if api.lastAlg != checksumAlgorithm || api.puts == 0 {
		t.Fatalf("single put algorithm = %q, puts = %d", api.lastAlg, api.puts)
	}
}

func TestErrorMapping(t *testing.T) {
	cause := errors.New("connection reset")
	cases := []struct {
		op   string
		give error
		want error
		call func(*Store) error
	}{
		{
			op: "HeadObject", give: &apiErr{code: "AccessDenied", status: 403, msg: "no"},
			want: fileserver.ErrPermission,
			call: func(s *Store) error { _, err := s.Stat(context.Background(), "k"); return err },
		},
		{
			op: "HeadObject", give: &apiErr{code: "ConditionalRequestConflict", status: 409, msg: "no"},
			want: fileserver.ErrConflict,
			call: func(s *Store) error { _, err := s.Stat(context.Background(), "k"); return err },
		},
		{
			op: "PutObject", give: &apiErr{code: "EntityTooLarge", status: 413, msg: "no"},
			want: fileserver.ErrTooLarge,
			call: func(s *Store) error {
				return s.Upload(context.Background(), "k", bytes.NewReader([]byte("x")))
			},
		},
		{
			op: "PutObject", give: &apiErr{code: "InternalError", status: 500, msg: "no"},
			want: fileserver.ErrIndeterminate,
			call: func(s *Store) error {
				return s.Upload(context.Background(), "k", bytes.NewReader([]byte("x")))
			},
		},
		{
			op: "PutObject", give: cause, want: fileserver.ErrIndeterminate,
			call: func(s *Store) error {
				return s.Upload(context.Background(), "k", bytes.NewReader([]byte("x")))
			},
		},
	}
	for _, tc := range cases {
		api := newMemAPI()
		api.errFor = map[string]error{tc.op: tc.give}
		s := newTestStore(t, api, Config{})
		err := tc.call(s)
		if !errors.Is(err, tc.want) || !errors.Is(err, tc.give) {
			t.Errorf("%s: error = %v, want both %v and the cause", tc.op, err, tc.want)
		}
	}
}

func TestDownloadDetectsReplacement(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{Concurrency: 2})
	payload := bytes.Repeat([]byte{'A'}, int(s.partSize)+10)
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.getsBeforeSwap = 1
	api.gets = 0
	api.mu.Unlock()

	var buf fileservertest.BufferAt
	err := s.Download(t.Context(), "obj", &buf)
	if !errors.Is(err, fileserver.ErrConflict) {
		t.Fatalf("Download = %v, want ErrConflict", err)
	}
	if bytes.Contains(buf.Bytes(), []byte{'B'}) {
		t.Fatal("download wrote bytes from the replacement object")
	}
	api.mu.Lock()
	gets := api.gets
	api.mu.Unlock()
	if gets < 2 {
		t.Fatalf("GetObject calls = %d, want a segmented download", gets)
	}
}

func TestUploadPartSize(t *testing.T) {
	s := &Store{partSize: 8 << 20}
	if got := s.uploadPartSize(1, false); got != s.partSize {
		t.Fatalf("unknown size part = %d", got)
	}
	if got := s.uploadPartSize(100, true); got != s.partSize {
		t.Fatalf("small object part = %d, want configured size", got)
	}
	huge := int64(9999) * (9 << 20)
	if got := s.uploadPartSize(huge, true); got != 9<<20 {
		t.Fatalf("huge object part = %d, want 9 MiB", got)
	}
}

func TestSignUploadAndDownload(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{Prefix: "pfx", Presigner: api})
	ctx := t.Context()

	up, err := s.SignUploadReq(ctx, "a/b",
		objectstore.WithExpiry(time.Hour),
		objectstore.WithContentType("text/plain"),
		objectstore.WithContentLength(3),
	)
	if err != nil {
		t.Fatal(err)
	}
	if up.Method != "PUT" || api.sign.putKey != "pfx/a/b" || api.sign.putCT != "text/plain" || !api.sign.putHas || api.sign.putLen != 3 {
		t.Fatalf("upload sign capture = %+v, request = %+v", api.sign, up)
	}
	if api.sign.expires != time.Hour {
		t.Fatalf("expiry = %s", api.sign.expires)
	}
	if up.Header.Get("Content-Type") != "text/plain" || up.Header.Get("Content-Length") != "3" {
		t.Fatalf("signed header = %v", up.Header)
	}
	if !strings.Contains(up.URL, "pfx/a/b") || time.Until(up.ExpiresAt) < 59*time.Minute {
		t.Fatalf("signed request = %+v", up)
	}
	up.Header.Set("Content-Type", "mutated")
	if api.sign.putCT != "text/plain" {
		t.Fatal("caller mutation leaked into the driver")
	}

	down, err := s.SignDownloadReq(ctx, "a/b", objectstore.WithDisposition("报告.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if down.Method != "GET" || api.sign.getKey != "pfx/a/b" {
		t.Fatalf("download sign = %+v capture %+v", down, api.sign)
	}
	if !strings.Contains(api.sign.getDisp, "filename*=UTF-8''") || !strings.Contains(api.sign.getDisp, "%E6%8A%A5%E5%91%8A.pdf") {
		t.Fatalf("disposition = %q", api.sign.getDisp)
	}

	if _, err := s.SignUploadReq(ctx, "a/b", objectstore.WithDisposition("x")); !errors.Is(err, fileserver.ErrInvalidArgument) {
		t.Fatalf("upload disposition error = %v", err)
	}
	if _, err := s.SignDownloadReq(ctx, "a/b", objectstore.WithContentLength(1)); !errors.Is(err, fileserver.ErrInvalidArgument) {
		t.Fatalf("download length error = %v", err)
	}
	if _, err := s.SignDownloadReq(ctx, "a/b", objectstore.WithDisposition("bad\nname")); !errors.Is(err, fileserver.ErrInvalidArgument) {
		t.Fatalf("control character disposition error = %v", err)
	}
}

func TestSignRequiresPresignerAndHidesURL(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{})
	_, err := s.SignUploadReq(t.Context(), "k")
	if !errors.Is(err, fileserver.ErrUnsupported) {
		t.Fatalf("missing presigner error = %v", err)
	}
	if api.sign.calls != 0 {
		t.Fatal("presign was called without a presigner")
	}

	secret := errors.New("https://bucket.example/k?X-Amz-Signature=secret")
	api.presignErr = secret
	s = newTestStore(t, api, Config{Presigner: api})
	_, err = s.SignDownloadReq(t.Context(), "k")
	if !errors.Is(err, secret) {
		t.Fatalf("presign error = %v, want the cause", err)
	}
	if strings.Contains(err.Error(), "X-Amz-Signature") || strings.Contains(err.Error(), "https://") {
		t.Fatalf("error text leaks the signed URL: %s", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = s.SignUploadReq(ctx, "k")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled sign = %v", err)
	}
}

func TestSignInvalidKeyDoesNotPresign(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{Presigner: api})
	if _, err := s.SignUploadReq(t.Context(), "../x"); !errors.Is(err, fileserver.ErrInvalidKey) {
		t.Fatalf("error = %v", err)
	}
	if api.sign.calls != 0 {
		t.Fatal("invalid key reached the presigner")
	}
}

func TestReadDoesNotCancelBodyWhenCallerContextEnds(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{})
	if err := s.Upload(t.Context(), "k", bytes.NewReader([]byte("abcdef"))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	rc, err := s.Read(ctx, "k", fileserver.WithRange(1, 3))
	cancel()
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bcd" {
		t.Fatalf("Read = %q", got)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestReadRejectsObjectReplacedDuringRangedRead pins the If-Match guard on the
// Read path. The same scenario is covered for Download by
// TestDownloadDetectsReplacement; without this test the guard can be deleted
// without any test noticing.
func TestReadRejectsObjectReplacedDuringRangedRead(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{})
	payload := bytes.Repeat([]byte{'A'}, 64)
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.getsBeforeSwap = 0
	api.gets = 0
	api.mu.Unlock()

	_, err := s.Read(t.Context(), "obj", fileserver.WithRange(0, 16))
	if !errors.Is(err, fileserver.ErrConflict) {
		t.Fatalf("Read = %v, want ErrConflict", err)
	}
}

// TestDownloadRejectsShortResponseBody pins the per-span length check. A body
// shorter than the requested range with an unchanged ETag is only detectable by
// comparing the copied byte count; If-Match does not fire. Truncated transfers
// report io.ErrUnexpectedEOF (retryable), not ErrConflict.
func TestDownloadRejectsShortResponseBody(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{})
	payload := bytes.Repeat([]byte{'A'}, 1000)
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.shortBody = 1
	api.mu.Unlock()

	var buf fileservertest.BufferAt
	err := s.Download(t.Context(), "obj", &buf)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !fileserver.Retryable(err) {
		t.Fatalf("Download = %v, want retryable io.ErrUnexpectedEOF", err)
	}
	got := len(buf.Bytes())
	if got >= len(payload) {
		t.Fatalf("wrote %d bytes, want fewer than %d", got, len(payload))
	}
	t.Logf("short response body: %d of %d bytes written before ErrConflict", got, len(payload))
}

// TestPrefixIsAppliedAndStripped checks that the prefix is added to the stored
// key while Stat keeps reporting the logical key.
func TestPrefixIsAppliedAndStripped(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{Prefix: "/tenant/a/"})
	body := []byte("prefixed")
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	if api.lastKey != "tenant/a/obj" {
		t.Fatalf("stored key = %q", api.lastKey)
	}
	info, err := s.Stat(t.Context(), "obj")
	if err != nil {
		t.Fatal(err)
	}
	if info.Key != "obj" || info.Size != int64(len(body)) {
		t.Fatalf("Stat = %+v", info)
	}
	seg := strings.Repeat("k", fileserver.MaxSegmentLength)
	long := seg + "/" + seg + "/" + seg + "/" + seg
	if err := s.Upload(t.Context(), long, bytes.NewReader(body)); !errors.Is(err, fileserver.ErrInvalidKey) {
		t.Fatalf("prefixed key error = %v, want ErrInvalidKey", err)
	}
}

// TestDownloadWholeObjectSkipsHead pins the fast path: with the default single
// concurrency and no range, one GET returns the whole object and no HEAD is sent.
func TestDownloadWholeObjectSkipsHead(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{})
	payload := bytes.Repeat([]byte{'z'}, 512)
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.gets, api.heads = 0, 0
	api.mu.Unlock()

	var buf fileservertest.BufferAt
	if err := s.Download(t.Context(), "obj", &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Fatalf("content mismatch: %d bytes", len(buf.Bytes()))
	}
	api.mu.Lock()
	gets, heads := api.gets, api.heads
	api.mu.Unlock()
	if gets != 1 || heads != 0 {
		t.Fatalf("GET=%d HEAD=%d, want 1/0", gets, heads)
	}
}

// TestDownloadSegmentedUsesHead pins the other side: parallel transfer needs the
// object size, so it HEADs once and issues one ranged GET per span.
func TestDownloadSegmentedUsesHead(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{Concurrency: 2})
	payload := bytes.Repeat([]byte{'z'}, int(s.partSize)+10)
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.gets, api.heads = 0, 0
	api.mu.Unlock()

	var buf fileservertest.BufferAt
	if err := s.Download(t.Context(), "obj", &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	api.mu.Lock()
	gets, heads := api.gets, api.heads
	api.mu.Unlock()
	if heads != 1 || gets < 2 {
		t.Fatalf("GET=%d HEAD=%d, want at least 2 GETs and exactly 1 HEAD", gets, heads)
	}
}

// TestMaxObjectSizeAppliesToUnknownLength pins the configured limit for the
// common case where the caller does not declare a size.
func TestMaxObjectSizeAppliesToUnknownLength(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{MaxObjectSize: 4})
	err := s.Upload(t.Context(), "small", bytes.NewReader([]byte("0123456789")))
	if !errors.Is(err, fileserver.ErrTooLarge) {
		t.Fatalf("Upload error = %v, want ErrTooLarge", err)
	}
	if _, err := s.Stat(t.Context(), "small"); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("oversized object became visible: %v", err)
	}
}

// TestMaxObjectSizeAppliesToMultipart also checks the limit after a multipart
// upload has started: the upload must be aborted, not completed.
func TestMaxObjectSizeAppliesToMultipart(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{MaxObjectSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+1)
	err := s.Upload(t.Context(), "big", bytes.NewReader(data))
	if !errors.Is(err, fileserver.ErrTooLarge) {
		t.Fatalf("Upload error = %v, want ErrTooLarge", err)
	}
	if api.uploadCount() != 0 {
		t.Fatalf("multipart uploads left = %d, want 0", api.uploadCount())
	}
	if api.aborts == 0 {
		t.Fatal("multipart upload was not aborted")
	}
}

// serialCheckWriter records whether WriteAt is ever entered concurrently.
type serialCheckWriter struct {
	buf        fileservertest.BufferAt
	active     atomic.Int32
	overlapped atomic.Bool
}

func (w *serialCheckWriter) WriteAt(p []byte, off int64) (int, error) {
	if w.active.Add(1) > 1 {
		w.overlapped.Store(true)
	}
	time.Sleep(time.Millisecond)
	n, err := w.buf.WriteAt(p, off)
	w.active.Add(-1)
	return n, err
}

// TestDownloadSerializesWriterAt pins that Download's network concurrency does
// not turn into concurrent calls to the caller's io.WriterAt.
func TestDownloadSerializesWriterAt(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{Concurrency: 2})
	data := bytes.Repeat([]byte("x"), int(s.partSize)+123)
	if err := s.Upload(t.Context(), "k", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}

	var w serialCheckWriter
	if err := s.Download(t.Context(), "k", &w); err != nil {
		t.Fatal(err)
	}
	if w.overlapped.Load() {
		t.Fatal("Download called WriteAt concurrently")
	}
	if got := w.buf.Bytes(); !bytes.Equal(got, data) {
		t.Fatalf("downloaded %d bytes, want %d", len(got), len(data))
	}
}

// TestProgressReportsEveryPartInOrder covers the per-part reports of a multipart
// upload and a segmented download. The single-request suite cases never reach them.
func TestProgressReportsEveryPartInOrder(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{PartSize: MinPartSize, Concurrency: 2})
	data := bytes.Repeat([]byte("p"), int(MinPartSize)*2+10)

	var up fileservertest.ProgressRecorder
	if err := s.Upload(t.Context(), "pg", bytes.NewReader(data), fileserver.WithProgress(&up)); err != nil {
		t.Fatal(err)
	}
	requireMonotonicProgress(t, up.Reports(), int64(len(data)))

	var down fileservertest.ProgressRecorder
	var buf fileservertest.BufferAt
	if err := s.Download(t.Context(), "pg", &buf, fileserver.WithProgress(&down)); err != nil {
		t.Fatal(err)
	}
	requireMonotonicProgress(t, down.Reports(), int64(len(data)))
}

func requireMonotonicProgress(t *testing.T, reports []fileserver.ProgressInfo, want int64) {
	t.Helper()
	if len(reports) < 2 {
		t.Fatalf("reports = %d, want one per part and a final report", len(reports))
	}
	var prev int64
	for i, p := range reports {
		if p.Transferred < prev || p.Transferred > want {
			t.Fatalf("report %d transferred %d after %d (total %d): not monotonic", i, p.Transferred, prev, want)
		}
		prev = p.Transferred
	}
	if last := reports[len(reports)-1]; last.Transferred != want || last.Total != want {
		t.Fatalf("final report = %+v, want transferred and total %d", last, want)
	}
}

type nilContentLengthAPI struct{ *memAPI }

func (a *nilContentLengthAPI) GetObject(ctx context.Context, in *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	out, err := a.memAPI.GetObject(ctx, in, optFns...)
	if out != nil {
		out.ContentLength = nil
	}
	return out, err
}

// TestDownloadWithoutContentLengthSucceeds pins the chunked-response case: a
// missing Content-Length means unknown, not zero.
func TestDownloadWithoutContentLengthSucceeds(t *testing.T) {
	api := newMemAPI()
	s, err := New(&nilContentLengthAPI{api}, Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("0123456789")
	if err := s.Upload(t.Context(), "k", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	var buf fileservertest.BufferAt
	if err := s.Download(t.Context(), "k", &buf); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got := buf.Bytes(); !bytes.Equal(got, payload) {
		t.Fatalf("content = %q", got)
	}
}

type nilBodyAPI struct{ *memAPI }

func (a *nilBodyAPI) GetObject(ctx context.Context, in *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	out, err := a.memAPI.GetObject(ctx, in, optFns...)
	if out != nil {
		out.Body = nil
	}
	return out, err
}

// TestDownloadNilBodyReturnsError makes sure Download does not panic on a
// malformed API response.
func TestDownloadNilBodyReturnsError(t *testing.T) {
	api := newMemAPI()
	s, err := New(&nilBodyAPI{api}, Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), "k", bytes.NewReader([]byte("x"))); err != nil {
		t.Fatal(err)
	}
	var buf fileservertest.BufferAt
	if err := s.Download(t.Context(), "k", &buf); err == nil {
		t.Fatal("Download accepted a nil response body")
	}
}

func TestContentDispositionEncodesRFC5987(t *testing.T) {
	got, err := contentDisposition("a:b@c=d; e")
	if err != nil {
		t.Fatal(err)
	}
	const want = "filename*=UTF-8''a%3Ab%40c%3Dd%3B%20e"
	if !strings.Contains(got, want) {
		t.Fatalf("content disposition = %q, want %q", got, want)
	}
	if !strings.HasPrefix(got, `attachment; filename="a:b@c=d; e"`) {
		t.Fatalf("missing ASCII fallback: %q", got)
	}
}

type emptyETagAPI struct{ *memAPI }

func (a *emptyETagAPI) HeadObject(ctx context.Context, in *awss3.HeadObjectInput, optFns ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	out, err := a.memAPI.HeadObject(ctx, in, optFns...)
	if out != nil {
		out.ETag = nil
	}
	return out, err
}

func (a *emptyETagAPI) GetObject(ctx context.Context, in *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	out, err := a.memAPI.GetObject(ctx, in, optFns...)
	if out != nil {
		out.ETag = nil
	}
	return out, err
}

// TestSegmentedDownloadRequiresETag pins the version guard: without an ETag a
// multi-span download cannot prove that all spans belong to one object version.
func TestSegmentedDownloadRequiresETag(t *testing.T) {
	api := newMemAPI()
	s, err := New(&emptyETagAPI{api}, Config{Bucket: "b", PartSize: 5 << 20, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte("x"), 5*(1<<20)+1)
	if err := s.Upload(t.Context(), "k", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	var buf fileservertest.BufferAt
	err = s.Download(t.Context(), "k", &buf)
	if !errors.Is(err, fileserver.ErrUnsupported) {
		t.Fatalf("Download error = %v, want ErrUnsupported", err)
	}
}

func TestReadHonorsCallerCancelDuringGet(t *testing.T) {
	started := make(chan struct{})
	api := &gateAPI{memAPI: newMemAPI(), hold: make(chan struct{}), started: started}
	s, err := New(api, Config{Bucket: "b", PartSize: MinPartSize})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), "k", bytes.NewReader([]byte("abcdef"))); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := s.Read(ctx, "k")
		errc <- err
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("GetObject did not start")
	}
	cancel()
	select {
	case err := <-errc:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Read error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read ignored cancellation of the caller context")
	}
}

func TestReadRejectsShortBody(t *testing.T) {
	api := newMemAPI()
	s := newTestStore(t, api, Config{})
	payload := bytes.Repeat([]byte{'A'}, 1000)
	if err := s.Upload(t.Context(), "obj", bytes.NewReader(payload)); err != nil {
		t.Fatal(err)
	}
	api.mu.Lock()
	api.shortBody = 1
	api.mu.Unlock()

	rc, err := s.Read(t.Context(), "obj")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()
	_, err = io.ReadAll(rc)
	if !errors.Is(err, io.ErrUnexpectedEOF) || !fileserver.Retryable(err) {
		t.Fatalf("Read = %v, want retryable io.ErrUnexpectedEOF", err)
	}
}

func TestHeadWithoutContentLengthIsAnError(t *testing.T) {
	s, err := New(&nilHeadLen{newMemAPI()}, Config{Bucket: "b"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), "k", bytes.NewReader([]byte("hello"))); err != nil {
		t.Fatal(err)
	}
	if info, err := s.Stat(t.Context(), "k"); err == nil || !errors.Is(err, errMissingLength) {
		t.Fatalf("Stat = %+v, %v; want missing content length", info, err)
	}
	var buf fileservertest.BufferAt
	if err := s.Download(t.Context(), "k", &buf, fileserver.WithRange(0, 2)); err == nil || len(buf.Bytes()) != 0 {
		t.Fatalf("ranged download = %v, wrote %d bytes", err, len(buf.Bytes()))
	}
	if _, err := s.Read(t.Context(), "k", fileserver.WithRange(0, 2)); err == nil {
		t.Fatal("ranged read accepted a head without content length")
	}
	if err := s.Download(t.Context(), "k", &buf); err != nil {
		t.Fatalf("whole-object download: %v", err)
	}
}

func TestUploadAcceptsPartWithoutChecksumEcho(t *testing.T) {
	api := newMemAPI()
	api.omitChecksum = true
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("y"), int(MinPartSize)+8)
	if err := s.Upload(t.Context(), "k", bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if api.aborts != 0 {
		t.Fatalf("aborts = %d, want 0", api.aborts)
	}
	var buf fileservertest.BufferAt
	if err := s.Download(t.Context(), "k", &buf); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), data) {
		t.Fatalf("downloaded %d bytes, want %d", len(buf.Bytes()), len(data))
	}
}

func TestUploadRejectsMismatchedPartChecksum(t *testing.T) {
	api := newMemAPI()
	api.badChecksum = true
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("y"), int(MinPartSize)+8)
	err := s.Upload(t.Context(), "k", bytes.NewReader(data))
	if !errors.Is(err, fileserver.ErrIntegrity) || !fileserver.Retryable(err) {
		t.Fatalf("Upload error = %v, want retryable ErrIntegrity", err)
	}
	if api.aborts == 0 {
		t.Fatal("mismatched checksum did not abort the upload")
	}
	if _, statErr := s.Stat(t.Context(), "k"); !errors.Is(statErr, fileserver.ErrNotFound) {
		t.Fatalf("rejected upload became visible: %v", statErr)
	}
}

func TestUploadPartFailureStaysDeterminateAfterAbort(t *testing.T) {
	api := newMemAPI()
	api.errFor = map[string]error{"UploadPart": context.DeadlineExceeded}
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+1)
	err := s.Upload(t.Context(), "k", bytes.NewReader(data))
	if errors.Is(err, fileserver.ErrIndeterminate) {
		t.Fatalf("aborted part upload reported indeterminate: %v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Upload error = %v, want the part failure", err)
	}
	if api.aborts == 0 {
		t.Fatal("multipart upload was not aborted")
	}
	if _, statErr := s.Stat(t.Context(), "k"); !errors.Is(statErr, fileserver.ErrNotFound) {
		t.Fatalf("failed upload became visible: %v", statErr)
	}
}

func TestCreateMultipartTimeoutIsIndeterminate(t *testing.T) {
	api := newMemAPI()
	api.errFor = map[string]error{"CreateMultipartUpload": context.DeadlineExceeded}
	s := newTestStore(t, api, Config{PartSize: MinPartSize})
	data := bytes.Repeat([]byte("x"), int(MinPartSize)+1)
	err := s.Upload(t.Context(), "k", bytes.NewReader(data))
	if !errors.Is(err, fileserver.ErrIndeterminate) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Upload error = %v, want ErrIndeterminate wrapping the timeout", err)
	}
	if api.aborts != 0 {
		t.Fatalf("abort calls = %d, want 0 without an upload ID", api.aborts)
	}
}

type gateAPI struct {
	*memAPI
	hold    chan struct{}
	started chan struct{}
}

func (a *gateAPI) GetObject(ctx context.Context, in *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	select {
	case <-a.started:
	default:
		close(a.started)
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-a.hold:
	}
	return a.memAPI.GetObject(ctx, in, optFns...)
}

type nilHeadLen struct{ *memAPI }

func (a *nilHeadLen) HeadObject(ctx context.Context, in *awss3.HeadObjectInput, optFns ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	out, err := a.memAPI.HeadObject(ctx, in, optFns...)
	if out != nil {
		out.ContentLength = nil
	}
	return out, err
}
