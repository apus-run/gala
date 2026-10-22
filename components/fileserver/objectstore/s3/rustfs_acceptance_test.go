package s3_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/apus-run/gala/components/fileserver"
	"github.com/apus-run/gala/components/fileserver/fileservertest"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestRustFSTLSTrust(t *testing.T) {
	if os.Getenv("RUSTFS_TEST_CA_FILE") == "" {
		t.Skip("TLS acceptance runner required")
	}
	client, _ := realClient(t)
	endpoint, err := url.Parse(os.Getenv("RUSTFS_TEST_ENDPOINT"))
	if err != nil || endpoint.Scheme != "https" {
		t.Fatal("HTTPS endpoint required")
	}
	t.Run("untrusted CA rejected", func(t *testing.T) {
		c := &http.Client{Timeout: 5 * time.Second}
		r, err := c.Get(endpoint.String() + "/health")
		if r != nil {
			r.Body.Close()
		}
		var unknown x509.UnknownAuthorityError
		if !errors.As(err, &unknown) {
			t.Fatalf("untrusted CA: %v", err)
		}
	})
	t.Run("wrong hostname rejected", func(t *testing.T) {
		trusted := client.Options().HTTPClient.(*http.Client).Transport.(*http.Transport)
		tr := trusted.Clone()
		tr.TLSClientConfig = trusted.TLSClientConfig.Clone()
		tr.TLSClientConfig.ServerName = "wrong.invalid"
		defer tr.CloseIdleConnections()
		r, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Get(endpoint.String() + "/health")
		if r != nil {
			r.Body.Close()
		}
		var mismatch x509.HostnameError
		if !errors.As(err, &mismatch) {
			t.Fatalf("hostname: %v", err)
		}
	})
	t.Run("TLS 1.1 rejected", func(t *testing.T) {
		c, err := tls.DialWithDialer(&net.Dialer{Timeout: 5 * time.Second}, "tcp", endpoint.Host, &tls.Config{MinVersion: tls.VersionTLS10, MaxVersion: tls.VersionTLS11})
		if c != nil {
			c.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "remote error: tls:") {
			t.Fatalf("old TLS probe must be rejected by the remote peer: %v", err)
		}
	})
	t.Run("anonymous object read denied", func(t *testing.T) {
		s := newRealStore(t, nil, "tls", 1)
		if err := s.Upload(t.Context(), "private", bytes.NewReader([]byte("secret"))); err != nil {
			t.Fatal(err)
		}
		r, err := client.Options().HTTPClient.Do(mustHTTPRequest(t, endpoint.String()+"/"+os.Getenv("RUSTFS_TEST_BUCKET")+"/tls/private"))
		if err != nil {
			t.Fatal("anonymous transport failed")
		}
		defer r.Body.Close()
		if r.StatusCode != http.StatusForbidden {
			t.Fatalf("anonymous GET=%d", r.StatusCode)
		}
	})
}
func mustHTTPRequest(t *testing.T, u string) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	if err != nil {
		t.Fatal("build HTTP request")
	}
	return r
}

type backupEntry struct {
	Key         string            `json:"key"`
	Size        int               `json:"size"`
	SHA256      string            `json:"sha256"`
	ContentType string            `json:"content_type"`
	Metadata    map[string]string `json:"metadata"`
}

func TestRustFSBackupRestore(t *testing.T) {
	phase := os.Getenv("RUSTFS_ACCEPTANCE_PHASE")
	if phase == "" {
		t.Skip("destructive isolated-volume runner required")
	}
	dir := os.Getenv("RUSTFS_BACKUP_DIR")
	if dir == "" {
		t.Fatal("backup directory required")
	}
	realClient(t)
	s := newRealStore(t, nil, "recovery", 3)
	var entries []backupEntry
	if phase == "backup" {
		for i, size := range []int{0, 4096, 2*(5<<20) + 123} {
			key := []string{"empty", "small", "multipart"}[i]
			data := payload(size)
			sum := sha256.Sum256(data)
			md := map[string]string{"purpose": "restore-acceptance", "kind": key}
			e := backupEntry{Key: key, Size: size, SHA256: hex.EncodeToString(sum[:]), ContentType: "application/octet-stream", Metadata: md}
			if err := s.Upload(t.Context(), key, bytes.NewReader(data), fileserver.WithSize(int64(size)), fileserver.WithContentType(e.ContentType), fileserver.WithMetadata(md)); err != nil {
				t.Fatal(err)
			}
			r, err := s.Read(t.Context(), key)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(r)
			closeErr := r.Close()
			if err != nil || closeErr != nil || !bytes.Equal(got, data) {
				t.Fatal("backup read failed")
			}
			if err := os.WriteFile(filepath.Join(dir, key+".bin"), got, 0o600); err != nil {
				t.Fatal(err)
			}
			entries = append(entries, e)
		}
		manifest, err := json.MarshalIndent(entries, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), manifest, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Log("quiescent snapshot: 3 acknowledged objects copied and hashed")
		return
	}
	if phase != "restore" {
		t.Fatal("unknown acceptance phase")
	}
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatal("incomplete manifest")
	}
	for _, e := range entries {
		if e.Key != "empty" && e.Key != "small" && e.Key != "multipart" {
			t.Fatal("unexpected manifest key")
		}
		if _, err := s.Stat(t.Context(), e.Key); !errors.Is(err, fileserver.ErrNotFound) {
			t.Fatalf("restore target must be empty: %v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Key+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if len(data) != e.Size || hex.EncodeToString(hash[:]) != e.SHA256 {
			t.Fatal("backup hash mismatch")
		}
		if err := s.Upload(t.Context(), e.Key, bytes.NewReader(data), fileserver.WithSize(int64(e.Size)), fileserver.WithContentType(e.ContentType), fileserver.WithMetadata(e.Metadata)); err != nil {
			t.Fatal(err)
		}
		requireBytes(t, s, e.Key, data)
		info, err := s.Stat(t.Context(), e.Key)
		if err != nil {
			t.Fatal(err)
		}
		if info.ContentType != e.ContentType || !reflect.DeepEqual(info.Metadata, e.Metadata) {
			t.Fatal("metadata was not restored")
		}
	}
	t.Log("restored 3 objects to an empty replacement volume; all sizes, hashes and metadata match")
}

// adminJSON signs the documented RustFS native admin endpoint. Secret-bearing
// bodies are never logged or copied into the evidence directory.
func adminJSON(t *testing.T, client *awss3.Client, method, path string, body any) int {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatal("encode admin request")
	}
	req, err := http.NewRequestWithContext(t.Context(), method, os.Getenv("RUSTFS_TEST_ENDPOINT")+path, bytes.NewReader(data))
	if err != nil {
		t.Fatal("build admin request")
	}
	req.Header.Set("Content-Type", "application/json")
	credentials, err := client.Options().Credentials.Retrieve(t.Context())
	if err != nil {
		t.Fatal("retrieve admin identity")
	}
	digest := sha256.Sum256(data)
	req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(digest[:]))
	if err := v4.NewSigner().SignHTTP(t.Context(), credentials, req, hex.EncodeToString(digest[:]), "s3", "us-east-1", time.Now()); err != nil {
		t.Fatal("sign admin request")
	}
	resp, err := client.Options().HTTPClient.Do(req)
	if err != nil {
		t.Fatal("admin transport failed")
	}
	defer resp.Body.Close()
	response, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode >= 400 {
		message := string(response)
		for _, sensitive := range []string{os.Getenv("RUSTFS_ACCESS_KEY"), os.Getenv("RUSTFS_SECRET_KEY")} {
			if sensitive != "" {
				message = strings.ReplaceAll(message, sensitive, "[redacted]")
			}
		}
		if fields, ok := body.(map[string]string); ok {
			if secret := fields["secretKey"]; secret != "" {
				message = strings.ReplaceAll(message, secret, "[redacted]")
			}
		}
		t.Logf("admin %s: status=%d response=%s", req.URL.Path, resp.StatusCode, message)
	}
	return resp.StatusCode
}
func TestRustFSTenantPolicy(t *testing.T) {
	if os.Getenv("RUSTFS_ACCEPTANCE_IAM") != "1" {
		t.Skip("isolated IAM acceptance runner required")
	}
	root, bucket := realClient(t)
	user := "user-" + freshPrefix(t)
	secret := freshPrefix(t) + freshPrefix(t)
	policyName := "policy-" + freshPrefix(t)
	policy := map[string]any{"Version": "2012-10-17", "Statement": []any{map[string]any{"Effect": "Allow", "Action": []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:AbortMultipartUpload"}, "Resource": []string{"arn:aws:s3:::" + bucket + "/tenant-allowed/*"}}}}
	operations := []struct {
		path string
		body any
	}{
		{"/rustfs/admin/v3/add-canned-policy?name=" + policyName, policy},
		{"/rustfs/admin/v3/add-user?accessKey=" + user, map[string]string{"secretKey": secret, "status": "enabled"}},
		{"/rustfs/admin/v3/set-user-or-group-policy?policyName=" + policyName + "&userOrGroup=" + user + "&isGroup=false", nil},
	}
	for _, op := range operations {
		if code := adminJSON(t, root, http.MethodPut, op.path, op.body); code/100 != 2 {
			t.Fatalf("IAM setup %s returned HTTP %d", strings.Split(op.path, "?")[0], code)
		}
	}
	scoped := awss3.New(awss3.Options{Region: "us-east-1", BaseEndpoint: root.Options().BaseEndpoint, UsePathStyle: true, RetryMaxAttempts: 1, HTTPClient: root.Options().HTTPClient,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: user, SecretAccessKey: secret}, nil
		})})
	allowed := newRealStore(t, scoped, "tenant-allowed", 2)
	data := payload(5*(1<<20) + 17)
	if err := allowed.Upload(t.Context(), "obj", bytes.NewReader(data)); err != nil {
		t.Fatalf("scoped multipart upload: %v", err)
	}
	requireBytes(t, allowed, "obj", data)
	var output fileservertest.BufferAt
	if err := allowed.Download(t.Context(), "obj", &output); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), data) {
		t.Fatal("scoped download mismatch")
	}
	if err := allowed.Remove(t.Context(), "obj"); err != nil {
		t.Fatal(err)
	}
	other := newRealStore(t, nil, "tenant-other", 1)
	if err := other.Upload(t.Context(), "private", bytes.NewReader([]byte("private"))); err != nil {
		t.Fatal(err)
	}
	denied := newRealStore(t, scoped, "tenant-other", 1)
	for _, op := range []struct {
		name string
		run  func() error
	}{
		{"read outside prefix", func() error {
			r, err := denied.Read(t.Context(), "private")
			if r != nil {
				r.Close()
			}
			return err
		}},
		{"write outside prefix", func() error { return denied.Upload(t.Context(), "private", bytes.NewReader([]byte("overwrite"))) }},
		{"delete outside prefix", func() error { return denied.Remove(t.Context(), "private") }},
	} {
		t.Run(op.name, func(t *testing.T) {
			if err := op.run(); !errors.Is(err, fileserver.ErrPermission) {
				t.Fatalf("scope violation: %v", err)
			}
		})
	}
	if code := adminJSON(t, scoped, http.MethodGet, "/rustfs/admin/v3/list-users", nil); code != http.StatusForbidden {
		t.Fatalf("scoped admin access returned %d", code)
	}
	requireBytes(t, other, "private", []byte("private"))
	t.Log("non-root identity: multipart/read/download/delete in tenant prefix allowed; cross-prefix and admin operations denied")
}
