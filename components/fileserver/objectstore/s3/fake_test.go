package s3

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// memAPI 是进程内的 S3 API，只服务于本包测试。它按对象键保存字节，并实现驱动依赖的条件写与分片操作。
type memAPI struct {
	mu sync.Mutex

	objects map[string]memObject
	uploads map[string]*memUpload
	nextID  int

	errFor map[string]error // 操作名 → 直接返回的错误，不改存储

	puts    int
	gets    int
	heads   int
	creates int
	aborts  int
	lastKey string
	lastAlg types.ChecksumAlgorithm

	// getsBeforeSwap ≥ 0 时，成功完成这么多次 GetObject 之后，下一次 Get 先替换对象再做 If-Match。
	getsBeforeSwap int
	// shortBody > 0 时，GetObject 比请求的范围少返回这么多字节，且 ETag 不变。
	shortBody int
	// omitChecksum 让 UploadPart 不回显 SHA-256。
	omitChecksum bool
	// badChecksum 让 UploadPart 回显一个与内容不符的 SHA-256。
	badChecksum bool

	presignErr error
	sign       signCapture
}

type memObject struct {
	data        []byte
	etag        string
	contentType string
	metadata    map[string]string
	mod         time.Time
}

type memUpload struct {
	key         string
	contentType string
	metadata    map[string]string
	parts       map[int32]memPart
}

type memPart struct {
	data []byte
	etag string
	sum  string
}

type signCapture struct {
	calls   int
	expires time.Duration
	putKey  string
	putCT   string
	putLen  int64
	putHas  bool
	getKey  string
	getDisp string
}

func newMemAPI() *memAPI {
	return &memAPI{
		objects:        make(map[string]memObject),
		uploads:        make(map[string]*memUpload),
		getsBeforeSwap: -1,
	}
}

var (
	_ API       = (*memAPI)(nil)
	_ Presigner = (*memAPI)(nil)
)

type apiErr struct {
	code   string
	status int
	msg    string
}

func (e *apiErr) Error() string                 { return e.code + ": " + e.msg }
func (e *apiErr) ErrorCode() string             { return e.code }
func (e *apiErr) ErrorMessage() string          { return e.msg }
func (e *apiErr) ErrorFault() smithy.ErrorFault { return smithy.FaultClient }
func (e *apiErr) HTTPStatusCode() int           { return e.status }

func errNoSuchKey() error {
	return &apiErr{code: "NoSuchKey", status: http.StatusNotFound, msg: "not found"}
}
func errNoSuchUpload() error {
	return &apiErr{code: "NoSuchUpload", status: http.StatusNotFound, msg: "no such upload"}
}
func errPrecondition() error {
	return &apiErr{code: "PreconditionFailed", status: http.StatusPreconditionFailed, msg: "precondition failed"}
}

func (a *memAPI) takeErr(op string) error {
	if a.errFor == nil {
		return nil
	}
	return a.errFor[op]
}

func (a *memAPI) PutObject(ctx context.Context, in *awss3.PutObjectInput, _ ...func(*awss3.Options)) (*awss3.PutObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readBody(in.Body)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.takeErr("PutObject"); err != nil {
		return nil, err
	}
	a.puts++
	a.lastAlg = in.ChecksumAlgorithm
	key := aws.ToString(in.Key)
	a.lastKey = key
	if aws.ToString(in.IfNoneMatch) == "*" {
		if _, ok := a.objects[key]; ok {
			return nil, errPrecondition()
		}
	}
	a.objects[key] = memObject{
		data:        data,
		etag:        etagOf(data),
		contentType: aws.ToString(in.ContentType),
		metadata:    maps.Clone(in.Metadata),
		mod:         time.Now(),
	}
	return &awss3.PutObjectOutput{ETag: aws.String(a.objects[key].etag)}, nil
}

func (a *memAPI) GetObject(ctx context.Context, in *awss3.GetObjectInput, _ ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.takeErr("GetObject"); err != nil {
		return nil, err
	}
	a.gets++
	key := aws.ToString(in.Key)
	o, ok := a.objects[key]
	if !ok {
		return nil, errNoSuchKey()
	}
	if a.getsBeforeSwap >= 0 {
		if a.getsBeforeSwap == 0 {
			o.data = bytes.Repeat([]byte{'B'}, len(o.data))
			o.etag = etagOf(o.data)
			o.mod = time.Now()
			a.objects[key] = o
			a.getsBeforeSwap = -1
		} else {
			a.getsBeforeSwap--
		}
	}
	if in.IfMatch != nil && aws.ToString(in.IfMatch) != o.etag {
		return nil, errPrecondition()
	}
	body, err := sliceRange(o.data, in.Range)
	if err != nil {
		return nil, err
	}
	declared := int64(len(body))
	if a.shortBody > 0 && len(body) > a.shortBody {
		body = body[:len(body)-a.shortBody]
	}
	return &awss3.GetObjectOutput{
		// 与真实 SDK 一致：响应体绑定请求 Context，取消后读取失败。
		Body:          &ctxBody{ctx: ctx, r: bytes.NewReader(body)},
		ContentLength: aws.Int64(declared),
		ContentType:   optionalString(o.contentType),
		ETag:          aws.String(o.etag),
		Metadata:      maps.Clone(o.metadata),
		LastModified:  aws.Time(o.mod),
	}, nil
}

func (a *memAPI) HeadObject(ctx context.Context, in *awss3.HeadObjectInput, _ ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.heads++
	if err := a.takeErr("HeadObject"); err != nil {
		return nil, err
	}
	o, ok := a.objects[aws.ToString(in.Key)]
	if !ok {
		return nil, errNoSuchKey()
	}
	return &awss3.HeadObjectOutput{
		ContentLength: aws.Int64(int64(len(o.data))),
		ContentType:   optionalString(o.contentType),
		ETag:          aws.String(o.etag),
		Metadata:      maps.Clone(o.metadata),
		LastModified:  aws.Time(o.mod),
	}, nil
}

func (a *memAPI) DeleteObject(ctx context.Context, in *awss3.DeleteObjectInput, _ ...func(*awss3.Options)) (*awss3.DeleteObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.takeErr("DeleteObject"); err != nil {
		return nil, err
	}
	delete(a.objects, aws.ToString(in.Key))
	return &awss3.DeleteObjectOutput{}, nil
}

func (a *memAPI) CreateMultipartUpload(ctx context.Context, in *awss3.CreateMultipartUploadInput, _ ...func(*awss3.Options)) (*awss3.CreateMultipartUploadOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.takeErr("CreateMultipartUpload"); err != nil {
		return nil, err
	}
	a.creates++
	a.lastAlg = in.ChecksumAlgorithm
	a.nextID++
	id := "upload-" + strconv.Itoa(a.nextID)
	a.uploads[id] = &memUpload{
		key:         aws.ToString(in.Key),
		contentType: aws.ToString(in.ContentType),
		metadata:    maps.Clone(in.Metadata),
		parts:       make(map[int32]memPart),
	}
	return &awss3.CreateMultipartUploadOutput{Bucket: in.Bucket, Key: in.Key, UploadId: aws.String(id)}, nil
}

func (a *memAPI) UploadPart(ctx context.Context, in *awss3.UploadPartInput, _ ...func(*awss3.Options)) (*awss3.UploadPartOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := readBody(in.Body)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.takeErr("UploadPart"); err != nil {
		return nil, err
	}
	u, ok := a.uploads[aws.ToString(in.UploadId)]
	if !ok || u.key != aws.ToString(in.Key) {
		return nil, errNoSuchUpload()
	}
	part := memPart{data: data, etag: etagOf(data), sum: sha256B64(data)}
	if got := aws.ToString(in.ChecksumSHA256); got != "" && got != part.sum {
		return nil, &apiErr{code: "BadDigest", status: http.StatusBadRequest, msg: "checksum"}
	}
	u.parts[aws.ToInt32(in.PartNumber)] = part
	out := &awss3.UploadPartOutput{ETag: aws.String(part.etag)}
	switch {
	case a.badChecksum:
		out.ChecksumSHA256 = aws.String("not-the-part-checksum")
	case !a.omitChecksum:
		out.ChecksumSHA256 = aws.String(part.sum)
	}
	return out, nil
}

func (a *memAPI) CompleteMultipartUpload(ctx context.Context, in *awss3.CompleteMultipartUploadInput, _ ...func(*awss3.Options)) (*awss3.CompleteMultipartUploadOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.takeErr("CompleteMultipartUpload"); err != nil {
		return nil, err
	}
	id := aws.ToString(in.UploadId)
	u, ok := a.uploads[id]
	if !ok || u.key != aws.ToString(in.Key) {
		return nil, errNoSuchUpload()
	}
	if aws.ToString(in.IfNoneMatch) == "*" {
		if _, exists := a.objects[u.key]; exists {
			return nil, errPrecondition()
		}
	}
	var buf bytes.Buffer
	if in.MultipartUpload != nil {
		for _, p := range in.MultipartUpload.Parts {
			got, ok := u.parts[aws.ToInt32(p.PartNumber)]
			if !ok || aws.ToString(p.ETag) != got.etag {
				return nil, &apiErr{code: "InvalidPart", status: http.StatusBadRequest, msg: "part etag"}
			}
			if p.ChecksumSHA256 != nil && aws.ToString(p.ChecksumSHA256) != got.sum {
				return nil, &apiErr{code: "InvalidPart", status: http.StatusBadRequest, msg: "part checksum"}
			}
			_, _ = buf.Write(got.data)
		}
	}
	data := bytes.Clone(buf.Bytes())
	a.objects[u.key] = memObject{
		data:        data,
		etag:        etagOf(data),
		contentType: u.contentType,
		metadata:    maps.Clone(u.metadata),
		mod:         time.Now(),
	}
	delete(a.uploads, id)
	return &awss3.CompleteMultipartUploadOutput{
		Bucket: in.Bucket,
		Key:    in.Key,
		ETag:   aws.String(a.objects[u.key].etag),
	}, nil
}

func (a *memAPI) AbortMultipartUpload(ctx context.Context, in *awss3.AbortMultipartUploadInput, _ ...func(*awss3.Options)) (*awss3.AbortMultipartUploadOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.takeErr("AbortMultipartUpload"); err != nil {
		return nil, err
	}
	a.aborts++
	id := aws.ToString(in.UploadId)
	u, ok := a.uploads[id]
	if !ok || u.key != aws.ToString(in.Key) {
		return nil, errNoSuchUpload()
	}
	delete(a.uploads, id)
	return &awss3.AbortMultipartUploadOutput{}, nil
}

func (a *memAPI) PresignPutObject(ctx context.Context, params *awss3.PutObjectInput, optFns ...func(*awss3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var opt awss3.PresignOptions
	for _, fn := range optFns {
		fn(&opt)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presignErr != nil {
		return nil, a.presignErr
	}
	a.sign.calls++
	a.sign.expires = opt.Expires
	a.sign.putKey = aws.ToString(params.Key)
	a.sign.putCT = aws.ToString(params.ContentType)
	a.sign.putLen = aws.ToInt64(params.ContentLength)
	a.sign.putHas = params.ContentLength != nil
	header := make(http.Header)
	if params.ContentType != nil {
		header.Set("Content-Type", aws.ToString(params.ContentType))
	}
	if params.ContentLength != nil {
		header.Set("Content-Length", strconv.FormatInt(aws.ToInt64(params.ContentLength), 10))
	}
	return &v4.PresignedHTTPRequest{
		URL:          "https://example.invalid/" + aws.ToString(params.Key) + "?X-Amz-Signature=test",
		Method:       http.MethodPut,
		SignedHeader: header,
	}, nil
}

func (a *memAPI) PresignGetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.PresignOptions)) (*v4.PresignedHTTPRequest, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var opt awss3.PresignOptions
	for _, fn := range optFns {
		fn(&opt)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.presignErr != nil {
		return nil, a.presignErr
	}
	a.sign.calls++
	a.sign.expires = opt.Expires
	a.sign.getKey = aws.ToString(params.Key)
	a.sign.getDisp = aws.ToString(params.ResponseContentDisposition)
	return &v4.PresignedHTTPRequest{
		URL:    "https://example.invalid/" + aws.ToString(params.Key) + "?X-Amz-Signature=test",
		Method: http.MethodGet,
	}, nil
}

func (a *memAPI) uploadCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.uploads)
}

func readBody(r io.Reader) ([]byte, error) {
	if r == nil {
		return []byte{}, nil
	}
	return io.ReadAll(r)
}

func sliceRange(data []byte, spec *string) ([]byte, error) {
	if spec == nil {
		return bytes.Clone(data), nil
	}
	rest, ok := strings.CutPrefix(aws.ToString(spec), "bytes=")
	if !ok {
		return nil, errors.New("malformed range")
	}
	lo, hi, ok := strings.Cut(rest, "-")
	if !ok {
		return nil, errors.New("malformed range")
	}
	start, err := strconv.ParseInt(lo, 10, 64)
	if err != nil {
		return nil, err
	}
	end, err := strconv.ParseInt(hi, 10, 64)
	if err != nil {
		return nil, err
	}
	if start < 0 || end < start || start > int64(len(data)) {
		return nil, &apiErr{code: "InvalidRange", status: http.StatusRequestedRangeNotSatisfiable, msg: "range"}
	}
	if start == int64(len(data)) {
		return []byte{}, nil
	}
	if end >= int64(len(data)) {
		end = int64(len(data)) - 1
	}
	return bytes.Clone(data[start : end+1]), nil
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return aws.String(s)
}

func etagOf(data []byte) string {
	sum := md5.Sum(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func sha256B64(data []byte) string {
	sum := sha256.Sum256(data)
	return encodeSHA256(sum)
}

// ctxBody 模拟 SDK 的响应体：请求 Context 取消后，读取失败。
type ctxBody struct {
	ctx context.Context
	r   io.Reader
}

func (b *ctxBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.r.Read(p)
}

func (b *ctxBody) Close() error { return nil }
