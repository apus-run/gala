package s3_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
	"github.com/apus-run/gala/components/fileserver/objectstore"
	driver "github.com/apus-run/gala/components/fileserver/objectstore/s3"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// This suite is opt-in. The runner supplies an isolated Docker volume and bucket.
var rustfsEnv struct {
	sync.Once
	client *awss3.Client
	bucket string
	err    error
}

func realClient(t *testing.T) (*awss3.Client, string) {
	t.Helper()
	endpoint := os.Getenv("RUSTFS_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set RUSTFS_TEST_ENDPOINT to run real RustFS tests")
	}
	rustfsEnv.Do(func() {
		key, secret := os.Getenv("RUSTFS_ACCESS_KEY"), os.Getenv("RUSTFS_SECRET_KEY")
		rustfsEnv.bucket = os.Getenv("RUSTFS_TEST_BUCKET")
		if key == "" || secret == "" || rustfsEnv.bucket == "" {
			rustfsEnv.err = errors.New("explicit test credentials and bucket are required")
			return
		}
		httpClient := &http.Client{Timeout: 30 * time.Second}
		if caFile := os.Getenv("RUSTFS_TEST_CA_FILE"); caFile != "" {
			pem, err := os.ReadFile(caFile)
			if err != nil {
				rustfsEnv.err = err
				return
			}
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(pem) {
				rustfsEnv.err = errors.New("invalid test CA")
				return
			}
			transport := http.DefaultTransport.(*http.Transport).Clone()
			transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
			httpClient.Transport = transport
		}
		rustfsEnv.client = awss3.New(awss3.Options{
			Region: "us-east-1", BaseEndpoint: aws.String(endpoint), UsePathStyle: true, RetryMaxAttempts: 1,
			Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				return aws.Credentials{AccessKeyID: key, SecretAccessKey: secret}, nil
			}),
			HTTPClient: httpClient,
		})
		if os.Getenv("RUSTFS_TEST_SKIP_CREATE") == "1" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, rustfsEnv.err = rustfsEnv.client.CreateBucket(ctx, &awss3.CreateBucketInput{Bucket: aws.String(rustfsEnv.bucket)})
		var code interface{ ErrorCode() string }
		if errors.As(rustfsEnv.err, &code) && code.ErrorCode() == "BucketAlreadyOwnedByYou" {
			rustfsEnv.err = nil
		}
	})
	if rustfsEnv.err != nil {
		t.Fatal(rustfsEnv.err)
	}
	return rustfsEnv.client, rustfsEnv.bucket
}
func newRealStore(t *testing.T, api driver.API, prefix string, concurrency int) *driver.Store {
	t.Helper()
	client, bucket := realClient(t)
	if api == nil {
		api = client
	}
	s, err := driver.New(api, driver.Config{Bucket: bucket, Prefix: prefix, PartSize: driver.MinPartSize, Concurrency: concurrency, Presigner: awss3.NewPresignClient(client)})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func freshPrefix(t *testing.T) string {
	t.Helper()
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	return "run-" + hex.EncodeToString(b[:])
}
func payload(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*31 + i/251) % 256)
	}
	return b
}
func requireBytes(t *testing.T, s *driver.Store, key string, want []byte) {
	t.Helper()
	r, err := s.Read(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	closeErr := r.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("read/close: %v / %v", err, closeErr)
	}
	if sha256.Sum256(got) != sha256.Sum256(want) || len(got) != len(want) {
		t.Fatalf("content mismatch: got %d want %d", len(got), len(want))
	}
}
func TestRustFSContract(t *testing.T) {
	realClient(t)
	fileservertest.RunStorageSuite(t, func(t *testing.T) fileserver.Storage { return newRealStore(t, nil, freshPrefix(t), 4) }, fileservertest.Capabilities{ContentType: true, Metadata: true, IfNotExists: true, ModTime: true, ETag: true})
}
func TestRustFSMultipartBoundaries(t *testing.T) {
	realClient(t)
	for _, size := range []int{0, 4096, int(driver.MinPartSize) - 1, int(driver.MinPartSize), int(driver.MinPartSize) + 1, 2*int(driver.MinPartSize) + 17} {
		for _, known := range []bool{false, true} {
			t.Run(fmt.Sprintf("size=%d/known=%t", size, known), func(t *testing.T) {
				s := newRealStore(t, nil, freshPrefix(t), 4)
				data := payload(size)
				var opts []fileserver.UploadOpt
				if known {
					opts = append(opts, fileserver.WithSize(int64(size)))
				}
				if err := s.Upload(t.Context(), "obj", bytes.NewReader(data), opts...); err != nil {
					t.Fatal(err)
				}
				requireBytes(t, s, "obj", data)
				var dst fileservertest.BufferAt
				if err := s.Download(t.Context(), "obj", &dst); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(dst.Bytes(), data) {
					t.Fatal("segmented download mismatch")
				}
			})
		}
	}
}
func TestRustFSMultipartConditionalRace(t *testing.T) {
	realClient(t)
	s := newRealStore(t, nil, freshPrefix(t), 2)
	const writers = 6
	start := make(chan struct{})
	errs := make(chan error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			err := s.Upload(t.Context(), "one", bytes.NewReader(bytes.Repeat([]byte{byte(i)}, int(driver.MinPartSize)+17)), fileserver.WithIfNotExists())
			errs <- err
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	wins := 0
	for err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, fileserver.ErrConflict) {
			t.Errorf("conditional write: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners=%d, want 1", wins)
	}
	r, err := s.Read(t.Context(), "one")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil || len(b) != int(driver.MinPartSize)+17 {
		t.Fatalf("winner content: %d, %v", len(b), err)
	}
	for _, v := range b {
		if v != b[0] {
			t.Fatal("mixed winner bytes")
		}
	}
}

// Embed the real SDK client: only the HEAD/GET scheduling seam is controlled.
type replaceAfterHead struct {
	*awss3.Client
	bucket, key string
	replacement []byte
	once        sync.Once
}

func (a *replaceAfterHead) HeadObject(ctx context.Context, in *awss3.HeadObjectInput, opts ...func(*awss3.Options)) (*awss3.HeadObjectOutput, error) {
	out, err := a.Client.HeadObject(ctx, in, opts...)
	if err != nil {
		return out, err
	}
	a.once.Do(func() {
		_, err = a.Client.PutObject(ctx, &awss3.PutObjectInput{Bucket: aws.String(a.bucket), Key: aws.String(a.key), Body: bytes.NewReader(a.replacement)})
	})
	return out, err
}
func TestRustFSVersionGuard(t *testing.T) {
	client, bucket := realClient(t)
	for _, rangedRead := range []bool{false, true} {
		t.Run(fmt.Sprint(rangedRead), func(t *testing.T) {
			prefix := freshPrefix(t)
			data := payload(2*int(driver.MinPartSize) + 1)
			s := newRealStore(t, nil, prefix, 3)
			if err := s.Upload(t.Context(), "obj", bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
			api := &replaceAfterHead{Client: client, bucket: bucket, key: prefix + "/obj", replacement: bytes.Repeat([]byte("z"), len(data))}
			guarded := newRealStore(t, api, prefix, 3)
			var err error
			if rangedRead {
				var r io.ReadCloser
				r, err = guarded.Read(t.Context(), "obj", fileserver.WithRange(1, 1024))
				if r != nil {
					r.Close()
				}
			} else {
				var dst fileservertest.BufferAt
				err = guarded.Download(t.Context(), "obj", &dst)
			}
			if !errors.Is(err, fileserver.ErrConflict) {
				t.Fatalf("replacement = %v, want ErrConflict", err)
			}
		})
	}
}
func TestRustFSChecksumEnforcement(t *testing.T) {
	client, bucket := realClient(t)
	key := freshPrefix(t) + "/invalid"
	_, err := client.PutObject(t.Context(), &awss3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader([]byte("bad")), ChecksumSHA256: aws.String(base64.StdEncoding.EncodeToString(make([]byte, 32)))})
	if err == nil {
		t.Fatal("backend accepted incorrect SHA-256")
	}
	var code interface{ ErrorCode() string }
	if !errors.As(err, &code) || (code.ErrorCode() != "BadDigest" && code.ErrorCode() != "XAmzContentSHA256Mismatch") {
		t.Fatalf("unexpected checksum error: %v", err)
	}
	_, err = client.HeadObject(t.Context(), &awss3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err == nil {
		t.Fatal("bad checksum published an object")
	}
}
func TestRustFSAbortAndPreserveOld(t *testing.T) {
	client, bucket := realClient(t)
	prefix := freshPrefix(t)
	s := newRealStore(t, nil, prefix, 2)
	if err := s.Upload(t.Context(), "obj", strings.NewReader("old")); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("input fault")
	r := fileservertest.FailAfter(bytes.NewReader(payload(3*int(driver.MinPartSize))), int64(driver.MinPartSize)+100, boom)
	if err := s.Upload(t.Context(), "obj", r); !errors.Is(err, boom) {
		t.Fatalf("failure=%v", err)
	}
	requireBytes(t, s, "obj", []byte("old"))
	out, err := client.ListMultipartUploads(t.Context(), &awss3.ListMultipartUploadsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix + "/")})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Uploads) != 0 {
		t.Fatalf("leaked uploads=%d", len(out.Uploads))
	}
}
func TestRustFSPresignedRequests(t *testing.T) {
	client, _ := realClient(t)
	s := newRealStore(t, nil, freshPrefix(t), 1)
	data := []byte("direct signed upload")
	up, err := s.SignUploadReq(t.Context(), "signed", objectstore.WithContentLength(int64(len(data))), objectstore.WithContentType("text/plain"))
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), up.Method, up.URL, bytes.NewReader(data))
	if err != nil {
		t.Fatal("build signed request")
	}
	req.Header = up.Header.Clone()
	resp, err := client.Options().HTTPClient.Do(req)
	if err != nil {
		t.Fatal("signed upload transport failed")
	}
	resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		t.Fatalf("signed PUT status=%d", resp.StatusCode)
	}
	requireBytes(t, s, "signed", data)
	down, err := s.SignDownloadReq(t.Context(), "signed", objectstore.WithDisposition("测试.txt"))
	if err != nil {
		t.Fatal(err)
	}
	req, err = http.NewRequestWithContext(t.Context(), down.Method, down.URL, nil)
	if err != nil {
		t.Fatal("build signed download")
	}
	req.Header = down.Header.Clone()
	resp, err = client.Options().HTTPClient.Do(req)
	if err != nil {
		t.Fatal("signed download transport failed")
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 200 || !bytes.Equal(b, data) || !strings.Contains(resp.Header.Get("Content-Disposition"), "filename*") {
		t.Fatalf("signed GET status=%d body=%d disposition=%q", resp.StatusCode, len(b), resp.Header.Get("Content-Disposition"))
	}
}
func TestRustFSConcurrentLoad(t *testing.T) {
	realClient(t)
	s := newRealStore(t, nil, freshPrefix(t), 2)
	data := payload(64 << 10)
	const count = 128
	started := time.Now()
	jobs := make(chan int)
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 8 {
		wg.Go(func() {
			for i := range jobs {
				key := fmt.Sprint(i)
				if err := s.Upload(t.Context(), key, bytes.NewReader(data), fileserver.WithSize(int64(len(data)))); err != nil {
					t.Errorf("upload: %v", err)
					failed.Add(1)
					continue
				}
				r, err := s.Read(t.Context(), key)
				if err != nil {
					t.Errorf("load read: %v", err)
					failed.Add(1)
					continue
				}
				got, readErr := io.ReadAll(r)
				closeErr := r.Close()
				if readErr != nil || closeErr != nil || !bytes.Equal(got, data) {
					t.Errorf("load verification failed: read=%v close=%v bytes=%d", readErr, closeErr, len(got))
					failed.Add(1)
				}
			}
		})
	}
	for i := range count {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	t.Logf("load: objects=%d bytes_each=%d workers=8 elapsed=%s failures=%d", count, len(data), time.Since(started), failed.Load())
}
func TestRustFSPersistence(t *testing.T) {
	phase := os.Getenv("RUSTFS_TEST_PHASE")
	if phase == "" {
		t.Skip("runner-controlled crash/restart phase")
	}
	realClient(t)
	prefix := "persistence"
	if phase == "write-degraded" || phase == "read-degraded" {
		prefix = "degraded"
	}
	s := newRealStore(t, nil, prefix, 4)
	for _, n := range []int{4096, 2*int(driver.MinPartSize) + 123} {
		data := payload(n)
		key := fmt.Sprint(n)
		switch phase {
		case "write", "write-degraded":
			if err := s.Upload(t.Context(), key, bytes.NewReader(data)); err != nil {
				t.Fatal(err)
			}
		case "read", "read-degraded":
			requireBytes(t, s, key, data)
		default:
			t.Fatal("unknown phase")
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRustFSLostCommitResponse(t *testing.T) {
	client, bucket := realClient(t)
	for _, mode := range []string{"put", "create", "complete"} {
		for _, retries := range []int{1, 3} {
			t.Run(fmt.Sprintf("%s/retries=%d", mode, retries), func(t *testing.T) {
				prefix := freshPrefix(t)
				var commits, attempts atomic.Int32
				opts := client.Options()
				opts.RetryMaxAttempts = retries
				var base http.RoundTripper = http.DefaultTransport
				if c, ok := opts.HTTPClient.(*http.Client); ok && c.Transport != nil {
					base = c.Transport
				}
				opts.HTTPClient = &http.Client{Timeout: 30 * time.Second, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					q := r.URL.Query()
					target := (mode == "put" && r.Method == http.MethodPut && !q.Has("partNumber")) ||
						(mode == "create" && r.Method == http.MethodPost && q.Has("uploads")) ||
						(mode == "complete" && r.Method == http.MethodPost && q.Has("uploadId"))
					if target {
						attempts.Add(1)
					}
					resp, err := base.RoundTrip(r)
					if err == nil && target && resp.StatusCode/100 == 2 {
						commits.Add(1)
						_, _ = io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						return nil, io.ErrUnexpectedEOF
					}
					return resp, err
				})}
				s, err := driver.New(awss3.New(opts), driver.Config{Bucket: bucket, Prefix: prefix, PartSize: driver.MinPartSize})
				if err != nil {
					t.Fatal(err)
				}
				size := 4096
				if mode != "put" {
					size = int(driver.MinPartSize) + 17
				}
				data := payload(size)
				err = s.Upload(t.Context(), "obj", bytes.NewReader(data), fileserver.WithIfNotExists())
				if !errors.Is(err, fileserver.ErrIndeterminate) {
					t.Fatalf("lost response = %v, want ErrIndeterminate", err)
				}
				if commits.Load() != 1 || attempts.Load() != 1 {
					t.Fatalf("commits=%d attempts=%d, want one attempt", commits.Load(), attempts.Load())
				}
				if mode != "create" {
					requireBytes(t, newRealStore(t, nil, prefix, 1), "obj", data)
					return
				}
				// Without the lost upload ID, only a bucket lifecycle rule can clean this
				// orphan in production. Enumerate this isolated test prefix to clean it here.
				pending, err := client.ListMultipartUploads(t.Context(), &awss3.ListMultipartUploadsInput{Bucket: aws.String(bucket), Prefix: aws.String(prefix + "/")})
				if err != nil {
					t.Fatal(err)
				}
				if len(pending.Uploads) != 1 {
					t.Fatalf("pending uploads=%d, want one", len(pending.Uploads))
				}
				for _, u := range pending.Uploads {
					if _, err := client.AbortMultipartUpload(t.Context(), &awss3.AbortMultipartUploadInput{Bucket: aws.String(bucket), Key: u.Key, UploadId: u.UploadId}); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
func TestRustFSRejectsWrongCredentials(t *testing.T) {
	client, bucket := realClient(t)
	for _, unknownKey := range []bool{false, true} {
		t.Run(fmt.Sprintf("unknown-key=%t", unknownKey), func(t *testing.T) {
			prefix := freshPrefix(t)
			if err := newRealStore(t, nil, prefix, 1).Upload(t.Context(), "existing", strings.NewReader("protected")); err != nil {
				t.Fatal(err)
			}
			// A fresh signer is essential: copying Client.Options shares the signing-key cache.
			opts := awss3.Options{Region: "us-east-1", BaseEndpoint: client.Options().BaseEndpoint,
				UsePathStyle: true, RetryMaxAttempts: 1, HTTPClient: client.Options().HTTPClient}
			opts.Credentials = aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
				key := os.Getenv("RUSTFS_ACCESS_KEY")
				if unknownKey {
					key = "unknown-test-key"
				}
				return aws.Credentials{AccessKeyID: key, SecretAccessKey: "intentionally-wrong-secret"}, nil
			})
			s, err := driver.New(awss3.New(opts), driver.Config{Bucket: bucket, Prefix: prefix})
			if err != nil {
				t.Fatal(err)
			}
			operations := []struct {
				name string
				run  func() error
			}{
				{"HEAD", func() error { _, err := s.Stat(t.Context(), "existing"); return err }},
				{"GET", func() error {
					r, err := s.Read(t.Context(), "existing")
					if r != nil {
						r.Close()
					}
					return err
				}},
				{"PUT", func() error { return s.Upload(t.Context(), "unauthorized", strings.NewReader("must not be accepted")) }},
				{"DELETE", func() error { return s.Remove(t.Context(), "existing") }},
			}
			for _, op := range operations {
				t.Run(op.name, func(t *testing.T) {
					if err := op.run(); !errors.Is(err, fileserver.ErrPermission) {
						t.Fatalf("wrong credentials: %s = %v, want ErrPermission", op.name, err)
					}
				})
			}
		})
	}
}

func TestRustFSClusterNoWriteQuorum(t *testing.T) {
	if os.Getenv("RUSTFS_TEST_PHASE") != "reject" {
		t.Skip("runner-controlled quorum phase")
	}
	realClient(t)
	s := newRealStore(t, nil, "persistence", 1)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	err := s.Upload(ctx, "rejected", strings.NewReader("must not commit without quorum"))
	var status interface{ HTTPStatusCode() int }
	if !errors.As(err, &status) || status.HTTPStatusCode() != http.StatusServiceUnavailable || !errors.Is(err, fileserver.ErrIndeterminate) {
		t.Fatalf("without quorum = %v, want HTTP 503 and ErrIndeterminate", err)
	}
	t.Logf("write rejected: %v", err)
}
func TestRustFSClusterRejectedWriteAbsent(t *testing.T) {
	if os.Getenv("RUSTFS_TEST_PHASE") != "recovered" {
		t.Skip("runner-controlled recovery phase")
	}
	realClient(t)
	s := newRealStore(t, nil, "persistence", 1)
	if _, err := s.Stat(t.Context(), "rejected"); !errors.Is(err, fileserver.ErrNotFound) {
		t.Fatalf("unacknowledged write after recovery: %v", err)
	}
}
