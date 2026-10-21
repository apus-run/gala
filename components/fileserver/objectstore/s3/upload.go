package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/internal/iox"
)

const mib = 1 << 20

// Upload 实现 fileserver.Uploader。
//
// 策略是"先读满一片"：输入在第一片内结束则使用单次 PutObject；否则创建分片上传，
// 所有片都成功后才 CompleteMultipartUpload。声明长度时，超长与短输入都在提交前失败并返回
// ErrSizeMismatch，已开始的分片上传会被 Abort。内存上界为 PartSize × Concurrency。
// PutObject、CreateMultipartUpload 与 UploadPart 都带 SHA-256 附加校验和；回执不把 ETag 当作内容 MD5。
func (s *Store) Upload(ctx context.Context, key string, r io.Reader, opts ...fileserver.UploadOpt) error {
	const op = "Upload"
	objKey, err := s.objectKey(key)
	if err != nil {
		return opErr(op, key, err)
	}
	uo, err := fileserver.NewUploadOptions(opts...)
	if err != nil {
		return opErr(op, key, err)
	}
	meta, err := newPutMeta(uo)
	if err != nil {
		return opErr(op, key, err)
	}
	if r == nil {
		return opErr(op, key, invalidArgument("reader is nil"))
	}
	if err := ctx.Err(); err != nil {
		return opErr(op, key, err)
	}
	size, hasSize := uo.Size()
	if hasSize && (size > maxObjectSize || (s.maxObject > 0 && size > s.maxObject)) {
		return opErr(op, key, tooLarge())
	}

	total := int64(-1)
	if hasSize {
		total = size
	}
	prog := newProgress(ctx, uo.Progress(), key, total)
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	u := uploadRun{
		s:           s,
		ctx:         runCtx,
		cancel:      cancel,
		objKey:      objKey,
		meta:        meta,
		size:        size,
		hasSize:     hasSize,
		ifNotExists: uo.IfNotExists(),
		prog:        prog,
	}
	written, err := u.run(r)
	if err != nil {
		return opErr(op, key, err)
	}
	prog.finish(written, written)
	return nil
}

// uploadRun 保存一次 Upload 的状态。主协程负责读取与派发，每个分片由一个 worker 上传。
type uploadRun struct {
	s           *Store
	ctx         context.Context // 本次调用的内部 Context，出错时取消
	cancel      context.CancelFunc
	objKey      string
	meta        putMeta
	size        int64 // 声明的长度；hasSize 为 false 时无意义
	hasSize     bool
	ifNotExists bool
	prog        *progress
	uploadID    string // 只由主协程在派发任何 worker 之前写入

	wg   sync.WaitGroup
	mu   sync.Mutex
	err  error
	done []types.CompletedPart
}

func (u *uploadRun) run(r io.Reader) (int64, error) {
	partSize := u.s.uploadPartSize(u.size, u.hasSize)
	bufSize := partSize
	if u.hasSize && u.size+1 < bufSize {
		bufSize = u.size + 1 // 小对象不需要分配整片缓冲
	}
	src := r
	if u.hasSize {
		// 最多读取 size+1 字节，用于在不无界读取的前提下发现超长输入。
		src = io.LimitReader(r, u.size+1)
	}
	c := &chunker{r: iox.Reader(u.ctx, src)}
	free := make(chan []byte, u.s.concurrency)
	for range u.s.concurrency {
		free <- nil // 空槽只限制在途片数；真正读取这一片时才分配缓冲。
	}

	var total int64
	for part := 1; ; part++ {
		buf, err := u.acquire(free)
		if err != nil {
			u.fail(err)
			break
		}
		if buf == nil {
			buf = make([]byte, int(bufSize))
		}
		n, last, err := c.next(buf)
		if err != nil {
			u.fail(err)
			break
		}
		total += int64(n)
		if err := u.checkTotal(total); err != nil {
			u.fail(err)
			break
		}
		if part == 1 && last {
			if err := u.single(buf[:n]); err != nil {
				return 0, err
			}
			return total, nil
		}
		if part > maxParts {
			u.fail(fmt.Errorf("%w: input needs more than %d parts", fileserver.ErrTooLarge, maxParts))
			break
		}
		if part == 1 {
			if err := u.begin(); err != nil {
				u.fail(err)
				break
			}
		}
		u.dispatch(part, buf, n, free)
		if last {
			break
		}
	}
	u.wg.Wait()
	return u.conclude(total)
}

// checkTotal 在读到 total 字节后检查长度：超过声明长度或配置上限时返回对应的错误。
func (u *uploadRun) checkTotal(total int64) error {
	if u.hasSize && total > u.size {
		return overlong(u.size)
	}
	if u.s.maxObject > 0 && total > u.s.maxObject {
		return tooLarge()
	}
	return nil
}

// conclude 在所有分片结束后提交对象。任何失败都会放弃已开始的分片上传。
func (u *uploadRun) conclude(total int64) (int64, error) {
	// 所有 worker 已退出，直接读取最终错误。
	if u.err != nil {
		return 0, u.abortWith(u.err)
	}
	if u.hasSize && total != u.size {
		return 0, u.abortWith(shortInput(u.size, total))
	}
	if err := u.complete(); err != nil {
		return 0, u.abortWith(err)
	}
	return total, nil
}

// single 使用单次 PutObject 提交整个对象。调用前没有任何分片在途。
func (u *uploadRun) single(data []byte) error {
	if u.hasSize && int64(len(data)) != u.size {
		return shortInput(u.size, int64(len(data)))
	}
	return u.s.putObject(u.ctx, u.objKey, data, u.meta, u.ifNotExists)
}

func (u *uploadRun) begin() error {
	in := &awss3.CreateMultipartUploadInput{
		Bucket:            aws.String(u.s.bucket),
		Key:               aws.String(u.objKey),
		ChecksumAlgorithm: checksumAlgorithm,
	}
	u.meta.applyCreate(in)
	out, err := u.s.api.CreateMultipartUpload(u.ctx, in, singleAttempt)
	if err != nil {
		// 超时或 5xx 时服务端可能已经建好上传，但客户端没有 UploadId，无法 Abort。
		return classifyWrite(err)
	}
	id := aws.ToString(out.UploadId)
	if id == "" {
		return errors.New("s3: CreateMultipartUpload returned no upload ID")
	}
	u.uploadID = id
	return nil
}

// dispatch 在 worker 中上传一片。上传结束后，缓冲被归还到 free。
// free 的容量等于缓冲总数，因此归还不会阻塞。
func (u *uploadRun) dispatch(part int, buf []byte, n int, free chan<- []byte) {
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		defer func() { free <- buf }()
		etag, sum, err := u.s.uploadPart(u.ctx, u.objKey, u.uploadID, part, buf[:n])
		if err != nil {
			u.fail(err)
			return
		}
		u.record(part, etag, sum, int64(n))
	}()
}

func (u *uploadRun) record(part int, etag, sum string, n int64) {
	u.mu.Lock()
	u.done = append(u.done, types.CompletedPart{
		PartNumber:     aws.Int32(int32(part)),
		ETag:           aws.String(etag),
		ChecksumSHA256: aws.String(sum),
	})
	u.mu.Unlock()
	u.prog.add(n)
}

func (u *uploadRun) complete() error {
	parts := u.done // 所有 worker 已退出；无需复制即可排序。
	slices.SortFunc(parts, func(a, b types.CompletedPart) int {
		return int(aws.ToInt32(a.PartNumber) - aws.ToInt32(b.PartNumber))
	})
	in := &awss3.CompleteMultipartUploadInput{
		Bucket:          aws.String(u.s.bucket),
		Key:             aws.String(u.objKey),
		UploadId:        aws.String(u.uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}
	if u.ifNotExists {
		in.IfNoneMatch = aws.String("*")
	}
	if _, err := u.s.api.CompleteMultipartUpload(u.ctx, in, singleAttempt); err != nil {
		return classifyComplete(err)
	}
	return nil
}

// acquire 取得一个空闲缓冲；调用方已取消时返回取消错误。
func (u *uploadRun) acquire(free <-chan []byte) ([]byte, error) {
	select {
	case buf := <-free:
		return buf, nil
	case <-u.ctx.Done():
		return nil, u.ctx.Err()
	}
}

// fail 记录第一个错误并取消其余工作。
func (u *uploadRun) fail(err error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.err == nil {
		u.err = err
		u.cancel()
	}
}

// abortWith 在存在分片上传时放弃它。Abort 的失败会附加到返回的错误上，残留由生命周期规则兜底。
func (u *uploadRun) abortWith(err error) error {
	if u.uploadID == "" {
		return err
	}
	if aerr := u.s.abortUpload(u.ctx, u.objKey, u.uploadID); aerr != nil {
		return errors.Join(err, fmt.Errorf("abort multipart upload: %w", aerr))
	}
	return err
}

// abortUpload 放弃未完成的分片上传。它使用基于 parent 派生的、不会被取消的短超时 Context，
// 因此调用方的 Context 已取消时仍能清理。
func (s *Store) abortUpload(parent context.Context, objKey, uploadID string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), abortTimeout)
	defer cancel()
	_, err := s.api.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(s.bucket),
		Key:      aws.String(objKey),
		UploadId: aws.String(uploadID),
	})
	if err == nil {
		return nil
	}
	if code, _ := apiError(err); code == "NoSuchUpload" {
		return nil
	}
	return classify(err)
}

// singleAttempt 防止 SDK 自动重试掩盖一次已经生效但响应丢失的提交。
// 例如条件创建的第一次写入成功，重试得到 412，无法据此判定整个调用未写入。
func singleAttempt(o *awss3.Options) { o.RetryMaxAttempts = 1 }

// putObject 使用单次 PutObject 上传 data；丢失回执时由调用方核对对象。
func (s *Store) putObject(ctx context.Context, objKey string, data []byte, meta putMeta, ifNotExists bool) error {
	in := &awss3.PutObjectInput{
		Bucket:            aws.String(s.bucket),
		Key:               aws.String(objKey),
		Body:              bytes.NewReader(data),
		ContentLength:     aws.Int64(int64(len(data))),
		ChecksumAlgorithm: checksumAlgorithm,
		ChecksumSHA256:    aws.String(encodeSHA256(sha256.Sum256(data))),
	}
	meta.applyPut(in)
	if ifNotExists {
		in.IfNoneMatch = aws.String("*")
	}
	if _, err := s.api.PutObject(ctx, in, singleAttempt); err != nil {
		return classifyWrite(err)
	}
	return nil
}

// uploadPart 上传一片。返回后端确认的 ETag 与 SHA-256（标准 base64）。
// 响应若回显摘要，必须与本地计算一致；不要求兼容后端回显。
func (s *Store) uploadPart(ctx context.Context, objKey, uploadID string, partNumber int, data []byte) (string, string, error) {
	sum := encodeSHA256(sha256.Sum256(data))
	out, err := s.api.UploadPart(ctx, &awss3.UploadPartInput{
		Bucket:            aws.String(s.bucket),
		Key:               aws.String(objKey),
		UploadId:          aws.String(uploadID),
		PartNumber:        aws.Int32(int32(partNumber)),
		Body:              bytes.NewReader(data),
		ContentLength:     aws.Int64(int64(len(data))),
		ChecksumAlgorithm: checksumAlgorithm,
		ChecksumSHA256:    aws.String(sum),
	})
	if err != nil {
		// 这一片还不是提交。调用方会 Abort；Abort 成功后对象没有变化，可以重试。
		return "", "", classify(err)
	}
	etag := aws.ToString(out.ETag)
	if etag == "" {
		return "", "", errors.New("s3: UploadPart returned no ETag")
	}
	got := aws.ToString(out.ChecksumSHA256)
	if got != "" && got != sum {
		return "", "", fmt.Errorf("%w: backend checksum does not match the part", fileserver.ErrIntegrity)
	}
	return etag, sum, nil
}

// uploadPartSize 计算分片大小。已知长度时，分片数不超过 MaxParts，并向上取整到 MiB；
// 未知长度时使用配置的分片大小。
func (s *Store) uploadPartSize(size int64, hasSize bool) int64 {
	if !hasSize {
		return s.partSize
	}
	ceilDiv := (size + maxParts - 1) / maxParts // ceil(size / MaxParts)
	ps := max(s.partSize, ceilDiv)
	return (ps + mib - 1) / mib * mib
}

// chunker 把输入切成片。next 会填满缓冲，并在填满后探测是否还有数据；
// 探测到的字节保存下来，留给下一片。因此调用方能判断当前片是否是最后一片。
type chunker struct {
	r     io.Reader
	carry []byte
	eof   bool
}

// next 填充 buf。last 为 true 表示 buf[:n] 是最后一片；只有输入为空时，它才可能为空。
func (c *chunker) next(buf []byte) (n int, last bool, err error) {
	n = copy(buf, c.carry)
	c.carry = c.carry[:0]
	if c.eof {
		return n, true, nil
	}
	for n < len(buf) {
		m, err := iox.ReadSome(c.r, buf[n:])
		n += m
		if err == io.EOF {
			c.eof = true
			return n, true, nil
		}
		if err != nil {
			return n, false, err
		}
	}

	// 一字节前瞻与填片遵守同一错误规则；即使返回了数据，也不能吞掉非 EOF 错误。
	var one [1]byte
	m, err := iox.ReadSome(c.r, one[:])
	if err != nil && err != io.EOF {
		return n, false, err
	}
	c.eof = err == io.EOF
	if m > 0 {
		c.carry = append(c.carry, one[0])
		return n, false, nil
	}
	return n, true, nil
}

// putMeta 是随对象一起持久化的内容类型与元数据。
type putMeta struct {
	contentType string
	metadata    map[string]string
}

func newPutMeta(uo fileserver.UploadOptions) (putMeta, error) {
	if err := validateContentType(uo.ContentType()); err != nil {
		return putMeta{}, err
	}
	md := uo.Metadata()
	if err := validateMetadata(md); err != nil {
		return putMeta{}, err
	}
	return putMeta{contentType: uo.ContentType(), metadata: md}, nil
}

func (m putMeta) applyPut(in *awss3.PutObjectInput) {
	if m.contentType != "" {
		in.ContentType = aws.String(m.contentType)
	}
	if len(m.metadata) > 0 {
		in.Metadata = m.metadata
	}
}

func (m putMeta) applyCreate(in *awss3.CreateMultipartUploadInput) {
	if m.contentType != "" {
		in.ContentType = aws.String(m.contentType)
	}
	if len(m.metadata) > 0 {
		in.Metadata = m.metadata
	}
}

func tooLarge() error {
	return fmt.Errorf("%w: object exceeds the configured size limit", fileserver.ErrTooLarge)
}

func overlong(size int64) error {
	return fmt.Errorf("%w: declared %d bytes, input is longer", fileserver.ErrSizeMismatch, size)
}

func shortInput(size, got int64) error {
	return fmt.Errorf("%w: declared %d bytes, got %d", fileserver.ErrSizeMismatch, size, got)
}
