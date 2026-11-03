package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/internal/iox"
)

// copyBufferSize is the per-worker copy buffer for downloads; it does not
// grow with the part size.
// copyBufferSize 是下载时每个 worker 的拷贝缓冲。它不随分片大小增长。
const copyBufferSize = 64 << 10

// Stat implements fileserver.Storage | Stat 实现 fileserver.Storage。
func (s *Store) Stat(ctx context.Context, key string) (*fileserver.ObjectInfo, error) {
	const op = "Stat"
	objKey, err := s.objectKey(key)
	if err != nil {
		return nil, opErr(op, key, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, opErr(op, key, err)
	}
	info, err := s.objectInfo(ctx, objKey, key)
	if err != nil {
		return nil, opErr(op, key, err)
	}
	return info, nil
}

func (s *Store) objectInfo(ctx context.Context, objKey, key string) (*fileserver.ObjectInfo, error) {
	out, err := s.api.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objKey)})
	if err != nil {
		return nil, classify(err)
	}
	if out.ContentLength == nil {
		return nil, errMissingLength
	}
	return &fileserver.ObjectInfo{
		Key:         key,
		Size:        aws.ToInt64(out.ContentLength),
		ModTime:     aws.ToTime(out.LastModified),
		ContentType: aws.ToString(out.ContentType),
		Metadata:    normalizeMetadata(out.Metadata),
		ETag:        aws.ToString(out.ETag),
	}, nil
}

// Remove implements fileserver.Storage. S3 deletion is idempotent; a
// missing object returns nil.
// Remove 实现 fileserver.Storage。S3 的删除本身是幂等的；对象不存在时返回 nil。
func (s *Store) Remove(ctx context.Context, key string) error {
	const op = "Remove"
	objKey, err := s.objectKey(key)
	if err != nil {
		return opErr(op, key, err)
	}
	if err := ctx.Err(); err != nil {
		return opErr(op, key, err)
	}
	_, err = s.api.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objKey)})
	if err != nil {
		if c := classify(err); !errors.Is(c, fileserver.ErrNotFound) {
			return opErr(op, key, c)
		}
	}
	return nil
}

// Read implements fileserver.Storage. The returned stream is the body of
// this GET and the caller must close it.
//
// The stream stays readable until Close and is not interrupted when the
// caller's Context is cancelled. The GET still honors the caller's
// cancellation and timeouts while in flight; only after the response
// arrives with the caller's Context still valid is it rebound to a
// Context cancelled only at Close. This matches the filestore file-stream
// semantics.
//
// Ranged reads HEAD first for the size and ETag, then issue the GET with
// If-Match: an object replaced between the two requests returns
// ErrConflict. Compatible services without ETags cannot provide this
// version guard and ranged reads return ErrUnsupported; whole-object
// reads do not depend on ETags. S3 has no random access; use Download
// with WithRange when random access is needed.
//
// Read 实现 fileserver.Storage。返回的流是这次 GET 的响应体，调用方必须关闭它。
//
// 流在 Close 之前一直可读，不随调用方 Context 的取消而中断。GET 进行期间仍然尊重
// 调用方的取消和超时；响应返回且调用方 Context 仍然有效之后，才改绑到只在 Close
// 时取消的 Context。这与 filestore 的文件流语义一致。
//
// 范围读取先 HEAD 取得大小与 ETag，再以 If-Match 发起 GET：对象在两次请求之间被替换时返回 ErrConflict。
// 没有 ETag 的兼容服务无法做这个版本守卫，范围读取返回 ErrUnsupported；整对象读取不依赖 ETag。
// S3 没有随机读能力；需要随机读时用 Download 加 WithRange。
func (s *Store) Read(ctx context.Context, key string, opts ...fileserver.ReadOpt) (io.ReadCloser, error) {
	result, err := s.open(ctx, "Read", key, opts...)
	if err != nil {
		return nil, err
	}
	return result.Body, nil
}

var _ fileserver.Opener = (*Store)(nil)

// Open returns the content and attributes of the GET response. Ranged
// reads use HEAD + If-Match so the object size and the GET share one
// version. Compatible backends without ContentLength cannot provide an
// exact size and return ErrUnsupported; Read still supports such backends.
// Open 返回 GET 响应的内容与属性。范围读取通过 HEAD + If-Match 保证对象大小与 GET 同版本。
// 缺少 ContentLength 的兼容后端无法提供准确大小，返回 ErrUnsupported；Read 仍支持这种后端。
func (s *Store) Open(ctx context.Context, key string, opts ...fileserver.ReadOpt) (*fileserver.OpenResult, error) {
	result, err := s.open(ctx, "Open", key, opts...)
	if err != nil {
		return nil, err
	}
	if result.Info.Size < 0 {
		return nil, opErr("Open", key, errors.Join(
			fmt.Errorf("%w: Open requires an object size", fileserver.ErrUnsupported), result.Body.Close()))
	}
	return result, nil
}

func (s *Store) open(ctx context.Context, op, key string, opts ...fileserver.ReadOpt) (*fileserver.OpenResult, error) {
	objKey, err := s.objectKey(key)
	if err != nil {
		return nil, opErr(op, key, err)
	}
	ro, err := fileserver.NewReadOptions(opts...)
	if err != nil {
		return nil, opErr(op, key, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, opErr(op, key, err)
	}

	size, fullSize, base := int64(-1), int64(-1), int64(0)
	in := &awss3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objKey)}
	if off, length := ro.Range(); off != 0 || length != -1 {
		info, err := s.objectInfo(ctx, objKey, key)
		if err != nil {
			return nil, opErr(op, key, err)
		}
		fullSize = info.Size
		base, size, err = iox.ResolveRange(fullSize, off, length)
		if err != nil {
			return nil, opErr(op, key, err)
		}
		if size == 0 {
			return &fileserver.OpenResult{
				Body: io.NopCloser(bytes.NewReader(nil)), Info: *info, Offset: base, Length: 0,
			}, nil
		}
		if info.ETag == "" {
			// Without an ETag there is no way to guarantee HEAD and GET
			// see the same version.
			// 没有 ETag 就无法保证 HEAD 与 GET 看到的是同一个版本。
			return nil, opErr(op, key, fmt.Errorf("%w: a ranged read requires an ETag for version consistency", fileserver.ErrUnsupported))
		}
		in.Range = aws.String(rangeHeader(base, size))
		in.IfMatch = ifMatch(info.ETag)
		// Keep the full size; the GET's ContentLength only covers the
		// returned range. The remaining attributes are read from the GET
		// response to avoid returning stale metadata from the HEAD phase.
		// 保存完整大小；GET 的 ContentLength 仅表示返回区间的长度。
		// 属性的其余部分从 GET 响应读取，避免返回 HEAD 阶段的旧元数据。
	}
	out, cancel, err := s.getDetached(ctx, in)
	if err != nil {
		return nil, opErr(op, key, classify(err))
	}
	if out.Body == nil {
		cancel()
		return nil, opErr(op, key, errors.New("s3: GetObject returned no body"))
	}
	if size < 0 && out.ContentLength != nil {
		size = aws.ToInt64(out.ContentLength)
	}
	if in.Range == nil {
		fullSize = size
	}
	return &fileserver.OpenResult{
		Body: &objectStream{ReadCloser: out.Body, cancel: cancel, op: op, key: key, left: size},
		Info: fileserver.ObjectInfo{
			Key: key, Size: fullSize, ContentType: aws.ToString(out.ContentType),
			ModTime: aws.ToTime(out.LastModified), ETag: aws.ToString(out.ETag),
			Metadata: normalizeMetadata(out.Metadata),
		},
		Offset: base, Length: size,
	}, nil
}

// getDetached issues the GET and bridges the caller's Context cancellation
// onto the request while it is in flight. When the GET returns
// successfully and the caller's Context is still valid, the bridge is
// dropped and the returned cancel is left for the caller's Close only;
// otherwise the response body is closed and the error returned.
// getDetached 发起 GET，并在它进行期间把调用方 Context 的取消桥接到请求上。GET 成功返回且
// 调用方 Context 仍然有效时，桥接即断开，返回的 cancel 只留给调用方 Close；否则关闭响应体并返回错误。
func (s *Store) getDetached(ctx context.Context, in *awss3.GetObjectInput) (*awss3.GetObjectOutput, context.CancelFunc, error) {
	reqCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, cancel)
	out, err := s.api.GetObject(reqCtx, in)
	stop()
	if err == nil && ctx.Err() == nil {
		return out, cancel, nil
	}
	if out != nil && out.Body != nil {
		_ = out.Body.Close()
	}
	cancel()
	if err == nil {
		err = ctx.Err()
	}
	return nil, nil, err
}

// objectStream binds the response body to the request's cancel function:
// Close closes the body and releases the request together. It makes the
// lifetime of Read depend on the caller's Close rather than on the
// caller's Context.
// objectStream 把响应体与请求的取消函数绑定在一起：Close 同时关闭响应体并释放请求。
// 它让 Read 的生命周期由调用方的 Close 决定，而不是由调用方的 Context 决定。
type objectStream struct {
	io.ReadCloser
	cancel context.CancelFunc
	op     string
	key    string
	left   int64 // bytes still expected; -1 when the length is unknown | 剩余预期字节；-1 表示长度未知
}

// Close closes the response body and cancels the request; repeated calls are safe | Close 关闭响应体并取消请求。重复调用是安全的。
func (s *objectStream) Close() error {
	err := s.ReadCloser.Close()
	s.cancel()
	return err
}

// Read validates the known length and wraps read errors into
// *fileserver.Error; io.EOF is left as-is.
// Read 校验已知长度，并把读取错误包进 *fileserver.Error。io.EOF 保持原样。
func (s *objectStream) Read(p []byte) (int, error) {
	n, err := s.ReadCloser.Read(p)
	if s.left >= 0 {
		s.left = max(0, s.left-int64(n))
		if (err == io.EOF || err == io.ErrUnexpectedEOF) && s.left > 0 {
			err = fmt.Errorf("%w: response ended with %d bytes still expected", io.ErrUnexpectedEOF, s.left)
		} else if err == io.ErrUnexpectedEOF {
			err = io.EOF
		}
	}
	if err == nil || err == io.EOF {
		return n, err
	}
	return n, &fileserver.Error{Op: s.op, Key: s.key, Err: err}
}

// Download implements fileserver.Downloader. Byte i of the object is
// written at offset i of w.
//
// With concurrency 1 and no range, a single GET is used: the response
// body is the whole object, no HEAD for the size is needed, and the
// single response is itself an atomic view of one version, so no version
// guard is required either.
//
// When segmentation is needed (Concurrency > 1), objects larger than one
// part are fetched concurrently in PartSize segments, each with If-Match,
// so an object replaced mid-download returns ErrConflict and the contents
// of two versions never mix. Concurrency is used only for network
// fetching: calls to WriteAt on w are serialized, so the caller need not
// provide a concurrency-safe WriterAt. On failure w may already hold
// partial content; Download neither truncates nor closes w.
// Download 实现 fileserver.Downloader。对象第 i 个字节写到 w 的偏移 i 处。
//
// 并发度为 1 且未指定范围时使用单次 GET：响应体就是完整对象，不需要先 HEAD 取大小，
// 单次响应本身是某一版本的原子视图，因此也无需版本守卫。
//
// 需要分段时（Concurrency > 1），大于一个分片的对象按 PartSize 分段并发获取，每段都带
// If-Match，因此对象在下载中被替换时返回 ErrConflict，两个版本的内容不会混合。
// 并发只用于网络获取：对 w 的 WriteAt 调用会被串行化，调用方无需提供并发安全的 WriterAt。
// 失败时 w 中可能已有部分内容；Download 不截断也不关闭 w。
func (s *Store) Download(ctx context.Context, key string, w io.WriterAt, opts ...fileserver.DownloadOpt) error {
	const op = "Download"
	objKey, err := s.objectKey(key)
	if err != nil {
		return opErr(op, key, err)
	}
	do, err := fileserver.NewDownloadOptions(opts...)
	if err != nil {
		return opErr(op, key, err)
	}
	if w == nil {
		return opErr(op, key, invalidArgument("writer is nil"))
	}
	if err := ctx.Err(); err != nil {
		return opErr(op, key, err)
	}

	off, length := do.Range()
	if off == 0 && length == -1 && s.concurrency == 1 {
		if err := s.downloadWhole(ctx, objKey, key, w, do.Progress()); err != nil {
			return opErr(op, key, err)
		}
		return nil
	}
	size, etag, err := s.head(ctx, objKey)
	if err != nil {
		return opErr(op, key, err)
	}
	start, n, err := iox.ResolveRange(size, off, length)
	if err != nil {
		return opErr(op, key, err)
	}
	spans := splitSpans(start, n, s.partSize)
	if len(spans) > 1 && etag == "" {
		return opErr(op, key, fmt.Errorf("%w: a segmented download requires an ETag", fileserver.ErrUnsupported))
	}
	prog := newProgress(ctx, do.Progress(), key, n)
	if len(spans) > 0 {
		if err := s.fetchSpans(ctx, objKey, w, etag, spans, prog); err != nil {
			return opErr(op, key, err)
		}
	}
	prog.finish(n, n)
	return nil
}

// downloadWhole fetches the whole object with one GET and writes it to w.
// It skips the HEAD and suits the default single-concurrency, no-range
// download. ContentLength is the length of this object version and a
// short read means the object was truncated in transit.
// downloadWhole 用一次 GET 取回整个对象并写入 w。它省掉了 HEAD，适用于默认的
// 单并发、无范围下载。ContentLength 是这一版对象的长度，短读视为对象在传输中被截断。
func (s *Store) downloadWhole(ctx context.Context, objKey, key string, w io.WriterAt, obs fileserver.Progress) error {
	out, err := s.api.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objKey),
	})
	if err != nil {
		return classify(err)
	}
	if out.Body == nil {
		return errors.New("s3: GetObject returned no body")
	}
	defer func() { _ = out.Body.Close() }()

	total := int64(-1)
	if out.ContentLength != nil {
		total = aws.ToInt64(out.ContentLength)
	}
	prog := newProgress(ctx, obs, key, total)
	n, err := io.CopyBuffer(io.NewOffsetWriter(w, 0), out.Body, make([]byte, copyBufferSize))
	if err != nil {
		return err
	}
	if total >= 0 && n != total {
		// The response of a single GET is an atomic view of one version;
		// falling short of ContentLength can only be truncation in
		// transit.
		// 单次 GET 的响应是某一版本的原子视图；短于 ContentLength 只能是传输截断。
		return fmt.Errorf("%w: response ended with %d of %d bytes", io.ErrUnexpectedEOF, n, total)
	}
	prog.finish(n, total)
	return nil
}

// span is one segment of the object: n bytes starting at off | span 是对象中的一段：从 off 开始的 n 个字节。
type span struct {
	off, n int64
}

// splitSpans cuts [start, start+n) into segments of partSize | splitSpans 把 [start, start+n) 按 partSize 切成若干段。
func splitSpans(start, n, partSize int64) []span {
	var spans []span
	for done := int64(0); done < n; done += partSize {
		spans = append(spans, span{off: start + done, n: min(partSize, n-done)})
	}
	return spans
}

// serializedWriterAt serializes concurrent WriteAt calls. io.WriterAt does
// not require concurrency safety, and the driver should not push the
// burden of concurrent writes onto the caller.
// serializedWriterAt 把并发 WriteAt 串行化。io.WriterAt 不要求并发安全，
// 驱动不应把并发写入的责任转嫁给调用方。
type serializedWriterAt struct {
	mu sync.Mutex
	w  io.WriterAt
}

// WriteAt delegates under the lock | WriteAt 在锁内委托底层写入.
func (s *serializedWriterAt) WriteAt(p []byte, off int64) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.WriteAt(p, off)
}

// fetchSpans fetches the segments concurrently and writes each at its
// offset in w. The first error cancels the remaining segments; all workers
// have exited before it returns. Writes to w are serialized.
// fetchSpans 并发获取各段，并写入 w 的对应偏移。第一个错误会取消其余段；
// 返回前所有 worker 都已退出。对 w 的写入是串行的。
func (s *Store) fetchSpans(ctx context.Context, objKey string, w io.WriterAt, etag string, spans []span, prog *progress) error {
	w = &serializedWriterAt{w: w}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		once     sync.Once
		firstErr error
		wg       sync.WaitGroup
	)
	fail := func(err error) {
		once.Do(func() {
			firstErr = err
			cancel()
		})
	}

	jobs := make(chan span)
	for range min(s.concurrency, len(spans)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, copyBufferSize)
			for sp := range jobs {
				if ctx.Err() != nil {
					continue
				}
				n, err := s.fetchSpan(ctx, objKey, w, etag, sp, buf)
				if err != nil {
					fail(err)
					continue
				}
				prog.add(n)
			}
		}()
	}
dispatch:
	for _, sp := range spans {
		select {
		case jobs <- sp:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()

	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// fetchSpan fetches one segment and writes it to w. The byte count is
// returned only after the write completes.
// fetchSpan 获取一段并写入 w。只有完整写入后才返回字节数。
func (s *Store) fetchSpan(ctx context.Context, objKey string, w io.WriterAt, etag string, sp span, buf []byte) (int64, error) {
	out, err := s.api.GetObject(ctx, &awss3.GetObjectInput{
		Bucket:  aws.String(s.bucket),
		Key:     aws.String(objKey),
		Range:   aws.String(rangeHeader(sp.off, sp.n)),
		IfMatch: ifMatch(etag),
	})
	if err != nil {
		return 0, classify(err)
	}
	if out.Body == nil {
		return 0, errors.New("s3: GetObject returned no body")
	}
	defer func() { _ = out.Body.Close() }()

	n, err := iox.CopyN(ctx, io.NewOffsetWriter(w, sp.off), out.Body, sp.n, buf)
	if err != nil {
		return 0, err
	}
	if n != sp.n {
		// If-Match already guarantees version consistency; falling short
		// of the requested range can only be truncation in transit.
		// If-Match 已保证版本一致；短于请求范围只能是传输截断。
		return 0, fmt.Errorf("%w: response ended with %d of %d bytes", io.ErrUnexpectedEOF, n, sp.n)
	}
	return n, nil
}

// head reads the size and ETag. The ETag binds the object version and feeds If-Match | head 读取大小与 ETag。ETag 绑定对象版本，用于 If-Match。
func (s *Store) head(ctx context.Context, objKey string) (int64, string, error) {
	out, err := s.api.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(objKey)})
	if err != nil {
		return 0, "", classify(err)
	}
	if out.ContentLength == nil {
		return 0, "", errMissingLength
	}
	return aws.ToInt64(out.ContentLength), aws.ToString(out.ETag), nil
}

var errMissingLength = errors.New("s3: HeadObject returned no content length")

func rangeHeader(start, n int64) string {
	return fmt.Sprintf("bytes=%d-%d", start, start+n-1)
}

// ifMatch turns an ETag into the If-Match parameter; the condition is
// omitted when there is no ETag.
// ifMatch 把 ETag 转换为 If-Match 参数；没有 ETag 时不发送该条件。
func ifMatch(etag string) *string {
	if etag == "" {
		return nil
	}
	return aws.String(etag)
}

// progress reports confirmed bytes monotonically. The counter and the
// callback share one lock dedicated to this call, so callbacks run
// serialized and progress never goes backwards. The lock does not belong
// to Store, so calling other Store methods from a callback cannot
// deadlock.
// progress 以单调递增的方式报告已确认的字节数。计数与回调共用本次调用专用的一把锁，
// 因此回调串行执行、进度不回退。这把锁不属于 Store，回调中调用 Store 的其它方法不会死锁。
type progress struct {
	obs   fileserver.Progress
	ctx   context.Context
	key   string
	total int64 // total used when reporting; -1 when unknown | 报告时使用的总量；未知时为 -1

	mu       sync.Mutex
	done     int64
	last     fileserver.ProgressInfo
	reported bool
}

func newProgress(ctx context.Context, obs fileserver.Progress, key string, total int64) *progress {
	return &progress{obs: obs, ctx: ctx, key: key, total: total}
}

// add counts n confirmed bytes into the progress and reports | add 把 n 个已确认字节计入进度并报告。
func (p *progress) add(n int64) {
	if p.obs == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done += n
	p.report(p.done, p.total)
}

// finish reports the final value; when it matches the previous report it
// does not report again.
// finish 报告最终值。与上一次报告相同时不重复报告。
func (p *progress) finish(transferred, total int64) {
	if p.obs == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.report(transferred, total)
}

// report must be called while holding mu | report 必须在 mu 内调用。
func (p *progress) report(transferred, total int64) {
	info := fileserver.ProgressInfo{Key: p.key, Transferred: transferred, Total: total}
	if p.reported && info == p.last {
		return
	}
	p.obs.Report(p.ctx, info)
	p.last, p.reported = info, true
}
