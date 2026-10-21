package fileservertest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/apus-run/gala/components/fileserver"
)

// Capabilities 声明后端对可选语义的支持。未声明的能力必须在写入任何数据之前返回
// ErrUnsupported，而不是静默忽略。
type Capabilities struct {
	ContentType bool // 支持 WithContentType，并通过 Stat 返回
	Metadata    bool // 支持 WithMetadata，并通过 Stat 返回
	IfNotExists bool // 支持 WithIfNotExists
	ModTime     bool // Stat 返回非零的 ModTime
	ETag        bool // Stat 返回非空的 FETag
}

// StorageFactory 为每个子测试创建一个新的、空的后端。
// 清理工作应通过 t.Cleanup 注册。
type StorageFactory func(t *testing.T) fileserver.Storage

// RunStorageSuite 对 Storage 实现运行共享契约测试，覆盖 doc.go 中的操作语义表、
// 选项拒绝规则、范围规则与 Key 规则。每个子测试使用一个新建的后端。
func RunStorageSuite(t *testing.T, newStorage StorageFactory, caps Capabilities) {
	t.Helper()
	for _, c := range storageCases() {
		t.Run(c.name, func(t *testing.T) {
			c.run(t, newStorage(t), caps)
		})
	}
}

type storageCase struct {
	name string
	run  func(t *testing.T, s fileserver.Storage, caps Capabilities)
}

func storageCases() []storageCase {
	return []storageCase{
		{"round trip preserves bytes", caseRoundTrip},
		{"empty object is a valid zero-byte input", caseEmptyObject},
		{"overwrite replaces the whole object", caseOverwrite},
		{"exact declared size is accepted", caseExactSize},
		{"short input with declared size fails before publish", caseShortInput},
		{"long input with declared size fails before publish", caseLongInput},
		{"failing reader keeps the previous object", caseFailingReader},
		{"content type and metadata are persisted or rejected", caseContentTypeAndMetadata},
		{"if-not-exists creates once and then conflicts", caseIfNotExists},
		{"concurrent if-not-exists writers: exactly one wins", caseIfNotExistsRace},
		{"invalid keys are rejected by every method", caseInvalidKeys},
		{"invalid arguments and options are rejected", caseInvalidArguments},
		{"missing objects report not found", caseMissing},
		{"remove is idempotent", caseRemoveIdempotent},
		{"read streams content and close succeeds", caseReadStream},
		{"read range boundaries follow the contract", caseReadRange},
		{"download writes bytes at their object offsets", caseDownloadOffsets},
		{"empty download range writes nothing", caseDownloadEmptyRange},
		{"upload does not close the reader", caseUploadDoesNotClose},
		{"canceled context returns the context error", caseCanceledContext},
		{"cancel during upload keeps the previous object", caseCancelDuringUpload},
		{"progress reports the final transferred bytes", caseProgress},
		{"stat reports key, size and optional fields honestly", caseStatFields},
		{"a key that is only a prefix is not an object", casePrefixIsNotObject},
	}
}

func caseRoundTrip(t *testing.T, s fileserver.Storage, _ Capabilities) {
	data := []byte("hello, fileserver")
	mustPut(t, s, "a/b.txt", data)

	info, err := s.Stat(t.Context(), "a/b.txt")
	requireNoErr(t, err, "Stat")
	if info.FKey != "a/b.txt" || info.FSize != int64(len(data)) {
		t.Fatalf("Stat = %+v, want key a/b.txt and size %d", info, len(data))
	}
	got, err := readAll(t, s, "a/b.txt")
	requireNoErr(t, err, "Read")
	requireContent(t, got, data, "Read")

	got, err = download(t, s, "a/b.txt")
	requireNoErr(t, err, "Download")
	requireContent(t, got, data, "Download")
}

func caseEmptyObject(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "empty", nil)
	mustPut(t, s, "empty-declared", []byte{}, fileserver.WithSize(0))

	for _, key := range []string{"empty", "empty-declared"} {
		info, err := s.Stat(t.Context(), key)
		requireNoErr(t, err, "Stat "+key)
		if info.FSize != 0 {
			t.Fatalf("Stat(%q).Size = %d, want 0", key, info.FSize)
		}
		got, err := readAll(t, s, key)
		requireNoErr(t, err, "Read "+key)
		requireContent(t, got, nil, "Read "+key)
	}
}

func caseOverwrite(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "k", []byte("a much longer first version"))
	mustPut(t, s, "k", []byte("short"))

	got, err := readAll(t, s, "k")
	requireNoErr(t, err, "Read")
	requireContent(t, got, []byte("short"), "Read after overwrite")
}

func caseExactSize(t *testing.T, s fileserver.Storage, _ Capabilities) {
	data := bytes.Repeat([]byte("x"), 70000)
	mustPut(t, s, "exact", data, fileserver.WithSize(int64(len(data))))

	got, err := readAll(t, s, "exact")
	requireNoErr(t, err, "Read")
	requireContent(t, got, data, "Read")
}

func caseShortInput(t *testing.T, s fileserver.Storage, _ Capabilities) {
	old := []byte("old-content")
	mustPut(t, s, "k", old)

	err := put(t, s, "k", []byte("short"), fileserver.WithSize(int64(len(old))))
	requireErr(t, err, fileserver.ErrSizeMismatch)
	requireContentAt(t, s, "k", old)

	err = put(t, s, "fresh", []byte("abc"), fileserver.WithSize(10))
	requireErr(t, err, fileserver.ErrSizeMismatch)
	requireMissing(t, s, "fresh")
}

func caseLongInput(t *testing.T, s fileserver.Storage, _ Capabilities) {
	old := []byte("old-content")
	mustPut(t, s, "k", old)

	err := put(t, s, "k", []byte("much longer than declared"), fileserver.WithSize(3))
	requireErr(t, err, fileserver.ErrSizeMismatch)
	requireContentAt(t, s, "k", old)

	err = put(t, s, "zero", []byte("x"), fileserver.WithSize(0))
	requireErr(t, err, fileserver.ErrSizeMismatch)
	requireMissing(t, s, "zero")
}

func caseFailingReader(t *testing.T, s fileserver.Storage, _ Capabilities) {
	old := []byte("previous version")
	mustPut(t, s, "k", old)

	boom := errors.New("injected reader failure")
	reader := FailAfter(bytes.NewReader([]byte("new version that fails halfway")), 8, boom)
	err := s.Upload(t.Context(), "k", reader)
	requireErr(t, err, boom)
	requireContentAt(t, s, "k", old)

	reader = FailAfter(bytes.NewReader([]byte("never published")), 4, boom)
	requireErr(t, s.Upload(t.Context(), "fresh", reader), boom)
	requireMissing(t, s, "fresh")
}

func caseContentTypeAndMetadata(t *testing.T, s fileserver.Storage, caps Capabilities) {
	data := []byte("payload")

	if caps.ContentType {
		mustPut(t, s, "ct", data, fileserver.WithContentType("text/plain; charset=utf-8"))
		info, err := s.Stat(t.Context(), "ct")
		requireNoErr(t, err, "Stat")
		if info.FContentType != "text/plain; charset=utf-8" {
			t.Fatalf("ContentType = %q, want text/plain; charset=utf-8", info.FContentType)
		}
	} else {
		err := put(t, s, "ct", data, fileserver.WithContentType("text/plain"))
		requireErr(t, err, fileserver.ErrUnsupported)
		requireMissing(t, s, "ct")
	}

	if caps.Metadata {
		mustPut(t, s, "md", data, fileserver.WithMetadata(map[string]string{"Color": "red"}))
		info, err := s.Stat(t.Context(), "md")
		requireNoErr(t, err, "Stat")
		if len(info.FMetadata) != 1 || info.FMetadata["color"] != "red" {
			t.Fatalf("Metadata = %v, want map[color:red]", info.FMetadata)
		}
	} else {
		err := put(t, s, "md", data, fileserver.WithMetadata(map[string]string{"color": "red"}))
		requireErr(t, err, fileserver.ErrUnsupported)
		requireMissing(t, s, "md")
	}
}

func caseIfNotExists(t *testing.T, s fileserver.Storage, caps Capabilities) {
	if !caps.IfNotExists {
		err := put(t, s, "once", []byte("first"), fileserver.WithIfNotExists())
		requireErr(t, err, fileserver.ErrUnsupported)
		requireMissing(t, s, "once")
		return
	}
	mustPut(t, s, "once", []byte("first"), fileserver.WithIfNotExists())

	err := put(t, s, "once", []byte("second"), fileserver.WithIfNotExists())
	requireErr(t, err, fileserver.ErrConflict)
	requireContentAt(t, s, "once", []byte("first"))
}

func caseIfNotExistsRace(t *testing.T, s fileserver.Storage, caps Capabilities) {
	if !caps.IfNotExists {
		t.Skip("backend does not support IfNotExists")
	}
	const writers = 8
	payloads := make([][]byte, writers)
	for i := range payloads {
		payloads[i] = bytes.Repeat([]byte{byte('a' + i)}, 256<<10)
	}

	errs := make([]error, writers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = s.Upload(t.Context(), "race", bytes.NewReader(payloads[i]), fileserver.WithIfNotExists())
		}()
	}
	close(start)
	wg.Wait()

	winner := -1
	for i, err := range errs {
		switch {
		case err == nil:
			if winner >= 0 {
				t.Fatalf("two writers succeeded (%d and %d)", winner, i)
			}
			winner = i
		case errors.Is(err, fileserver.ErrConflict):
		default:
			t.Fatalf("writer %d: unexpected error %v", i, err)
		}
	}
	if winner < 0 {
		t.Fatal("no writer succeeded")
	}
	got, err := readAll(t, s, "race")
	requireNoErr(t, err, "Read")
	requireContent(t, got, payloads[winner], "object after race")
}

func caseInvalidKeys(t *testing.T, s fileserver.Storage, _ Capabilities) {
	keys := []string{
		"", "/a", "a/", "a//b", "a/./b", "a/../b", "..", ".", `a\b`, "a:b",
		"a*b", "a?b", `a"b`, "a<b", "a>b", "a|b",
		"dir/CON", "dir/nul.txt", "dir/trailing.", "dir/trailing ",
		"a\x00b", "a\x7fb", "\xff", strings.Repeat("k", 1025),
		"dir/" + strings.Repeat("n", 256), "s//x",
	}
	ctx := t.Context()
	for _, key := range keys {
		err := put(t, s, key, []byte("data"))
		requireErr(t, err, fileserver.ErrInvalidKey)
		_, err = s.Stat(ctx, key)
		requireErr(t, err, fileserver.ErrInvalidKey)
		_, err = readAll(t, s, key)
		requireErr(t, err, fileserver.ErrInvalidKey)
		_, err = download(t, s, key)
		requireErr(t, err, fileserver.ErrInvalidKey)
		requireErr(t, s.Remove(ctx, key), fileserver.ErrInvalidKey)
	}
}

func caseInvalidArguments(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "present", []byte("0123456789"))
	ctx := t.Context()

	requireErr(t, put(t, s, "present", []byte("x"), fileserver.WithSize(-1)), fileserver.ErrInvalidArgument)
	requireErr(t, put(t, s, "present", []byte("x"), fileserver.WithMetadata(map[string]string{"": "v"})), fileserver.ErrInvalidArgument)
	_, err := download(t, s, "present", fileserver.WithRange(-1, 2))
	requireErr(t, err, fileserver.ErrInvalidArgument)
	_, err = readAll(t, s, "present", fileserver.WithRange(0, -2))
	requireErr(t, err, fileserver.ErrInvalidArgument)
	requireErr(t, s.Upload(ctx, "present", nil), fileserver.ErrInvalidArgument)
	requireErr(t, s.Download(ctx, "present", nil), fileserver.ErrInvalidArgument)
}

func caseMissing(t *testing.T, s fileserver.Storage, _ Capabilities) {
	requireMissing(t, s, "nope")
	_, err := readAll(t, s, "nope")
	requireErr(t, err, fileserver.ErrNotFound)
	_, err = download(t, s, "nope")
	requireErr(t, err, fileserver.ErrNotFound)
}

func caseRemoveIdempotent(t *testing.T, s fileserver.Storage, _ Capabilities) {
	ctx := t.Context()
	requireNoErr(t, s.Remove(ctx, "never-existed"), "Remove of missing object")

	mustPut(t, s, "r", []byte("data"))
	requireNoErr(t, s.Remove(ctx, "r"), "Remove")
	requireMissing(t, s, "r")
	requireNoErr(t, s.Remove(ctx, "r"), "second Remove")
}

func caseReadStream(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "stream", []byte("hello world"))
	rc, err := s.Read(t.Context(), "stream")
	requireNoErr(t, err, "Read")

	head := make([]byte, 5)
	_, err = io.ReadFull(rc, head)
	requireNoErr(t, err, "ReadFull")
	requireContent(t, head, []byte("hello"), "first part")
	rest, err := io.ReadAll(rc)
	requireNoErr(t, err, "ReadAll")
	requireContent(t, rest, []byte(" world"), "rest")
	requireNoErr(t, rc.Close(), "Close")
}

func caseReadRange(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "range", []byte("0123456789"))
	tests := []struct {
		off, length int64
		want        string
	}{
		{2, 3, "234"},
		{8, -1, "89"},
		{8, 100, "89"},
		{0, -1, "0123456789"},
		{0, 0, ""},
		{10, 0, ""},
		{10, -1, ""},
	}
	for _, tt := range tests {
		got, err := readAll(t, s, "range", fileserver.WithRange(tt.off, tt.length))
		requireNoErr(t, err, "Read range")
		requireContent(t, got, []byte(tt.want), "Read range")
	}
	_, err := readAll(t, s, "range", fileserver.WithRange(11, -1))
	requireErr(t, err, fileserver.ErrInvalidArgument)
}

func caseDownloadOffsets(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "dl", []byte("0123456789"))

	var buf BufferAt
	requireNoErr(t, s.Download(t.Context(), "dl", &buf, fileserver.WithRange(3, 4)), "Download range")
	got := buf.Bytes()
	if len(got) < 7 || string(got[3:7]) != "3456" {
		t.Fatalf("range download wrote %q, want bytes 3..6 to be \"3456\"", got)
	}
}

func caseDownloadEmptyRange(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "dl", []byte("0123456789"))
	got, err := download(t, s, "dl", fileserver.WithRange(10, -1))
	requireNoErr(t, err, "Download at end")
	requireContent(t, got, nil, "Download at end")
}

func caseUploadDoesNotClose(t *testing.T, s fileserver.Storage, _ Capabilities) {
	tracked := &TrackedReader{Reader: bytes.NewReader([]byte("data"))}
	requireNoErr(t, s.Upload(t.Context(), "k", tracked), "Upload")
	if tracked.Closed() {
		t.Fatal("Upload closed the reader; the caller owns it")
	}
}

func caseCanceledContext(t *testing.T, s fileserver.Storage, _ Capabilities) {
	mustPut(t, s, "present", []byte("data"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	requireErr(t, s.Upload(ctx, "fresh", bytes.NewReader([]byte("x"))), context.Canceled)
	requireErr(t, s.Download(ctx, "present", &BufferAt{}), context.Canceled)
	_, err := s.Read(ctx, "present")
	requireErr(t, err, context.Canceled)
	_, err = s.Stat(ctx, "present")
	requireErr(t, err, context.Canceled)
	requireErr(t, s.Remove(ctx, "present"), context.Canceled)
	requireMissing(t, s, "fresh")
}

func caseCancelDuringUpload(t *testing.T, s fileserver.Storage, _ Capabilities) {
	old := []byte("keep me")
	mustPut(t, s, "k", old)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &CancelAfterReader{R: bytes.NewReader(bytes.Repeat([]byte("n"), 1<<20)), Cancel: cancel}
	err := s.Upload(ctx, "k", reader)
	requireErr(t, err, context.Canceled)
	requireContentAt(t, s, "k", old)
}

func caseProgress(t *testing.T, s fileserver.Storage, _ Capabilities) {
	data := bytes.Repeat([]byte("p"), 300000)
	var up ProgressRecorder
	mustPut(t, s, "pg", data, fileserver.WithProgress(&up))
	requireProgress(t, up.Reports(), "pg", int64(len(data)), "Upload")

	var down ProgressRecorder
	var buf BufferAt
	requireNoErr(t, s.Download(t.Context(), "pg", &buf, fileserver.WithProgress(&down)), "Download")
	requireProgress(t, down.Reports(), "pg", int64(len(data)), "Download")
}

func caseStatFields(t *testing.T, s fileserver.Storage, caps Capabilities) {
	mustPut(t, s, "plain", []byte("12345"))
	info, err := s.Stat(t.Context(), "plain")
	requireNoErr(t, err, "Stat")
	if info.FKey != "plain" || info.FSize != 5 {
		t.Fatalf("Stat = %+v, want key plain and size 5", info)
	}
	if !caps.ModTime && !info.FModTime.IsZero() {
		t.Logf("backend reports ModTime although Capabilities.ModTime is false")
	}
	if caps.ModTime && info.FModTime.IsZero() {
		t.Fatal("Stat.ModTime is zero, but the backend declares ModTime support")
	}
	if !caps.ContentType && info.FContentType != "" {
		t.Fatalf("ContentType = %q for an object without content type", info.FContentType)
	}
	if !caps.Metadata && info.FMetadata != nil {
		t.Fatalf("Metadata = %v, want nil when unsupported", info.FMetadata)
	}
	if caps.ETag && info.FETag == "" {
		t.Fatal("Stat.ETag is empty, but the backend declares ETag support")
	}
	if !caps.ETag && info.FETag != "" {
		t.Fatalf("ETag = %q for a backend without ETag support", info.FETag)
	}
}

func casePrefixIsNotObject(t *testing.T, s fileserver.Storage, _ Capabilities) {
	data := []byte("inside")
	mustPut(t, s, "dir/file", data)

	requireMissing(t, s, "dir")
	_, err := readAll(t, s, "dir")
	requireErr(t, err, fileserver.ErrNotFound)
	_, err = download(t, s, "dir")
	requireErr(t, err, fileserver.ErrNotFound)
	requireNoErr(t, s.Remove(t.Context(), "dir"), "Remove of prefix")

	got, err := readAll(t, s, "dir/file")
	requireNoErr(t, err, "Read child")
	requireContent(t, got, data, "child after prefix Remove")
}

// ---- 共享辅助函数 ----

func put(t *testing.T, s fileserver.Storage, key string, data []byte, opts ...fileserver.UploadOpt) error {
	t.Helper()
	return s.Upload(t.Context(), key, bytes.NewReader(data), opts...)
}

func mustPut(t *testing.T, s fileserver.Storage, key string, data []byte, opts ...fileserver.UploadOpt) {
	t.Helper()
	if err := put(t, s, key, data, opts...); err != nil {
		t.Fatalf("Upload(%q): %v", key, err)
	}
}

func readAll(t *testing.T, s fileserver.Storage, key string, opts ...fileserver.ReadOpt) ([]byte, error) {
	t.Helper()
	rc, err := s.Read(t.Context(), key, opts...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func download(t *testing.T, s fileserver.Storage, key string, opts ...fileserver.DownloadOpt) ([]byte, error) {
	t.Helper()
	var buf BufferAt
	err := s.Download(t.Context(), key, &buf, opts...)
	return buf.Bytes(), err
}

func requireContentAt(t *testing.T, s fileserver.Storage, key string, want []byte) {
	t.Helper()
	got, err := readAll(t, s, key)
	requireNoErr(t, err, "Read "+key)
	requireContent(t, got, want, "content of "+key)
}

func requireMissing(t *testing.T, s fileserver.Storage, key string) {
	t.Helper()
	if _, err := s.Stat(t.Context(), key); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("Stat(%q) = %v, want ErrNotFound", key, err)
	}
}

func requireNoErr(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected error: %v", what, err)
	}
}

func requireErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("error = %v, want errors.Is(err, %v)", err, target)
	}
}

func requireContent(t *testing.T, got, want []byte, what string) {
	t.Helper()
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: got %q (%d bytes), want %q (%d bytes)", what, got, len(got), want, len(want))
	}
}

func requireProgress(t *testing.T, reports []fileserver.ProgressInfo, key string, want int64, what string) {
	t.Helper()
	if len(reports) == 0 {
		t.Fatalf("%s: no progress reported", what)
	}
	var prev int64
	for i, p := range reports {
		if p.Key != key {
			t.Fatalf("%s: report %d has key %q, want %q", what, i, p.Key, key)
		}
		if p.Transferred < prev || p.Transferred > want {
			t.Fatalf("%s: report %d transferred %d (previous %d, total %d): not monotonic or out of range", what, i, p.Transferred, prev, want)
		}
		prev = p.Transferred
	}
	last := reports[len(reports)-1]
	if last.Transferred != want || last.Total != want {
		t.Fatalf("%s: final report = %+v, want transferred and total %d", what, last, want)
	}
}
