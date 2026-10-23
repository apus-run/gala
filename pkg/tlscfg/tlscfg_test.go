package tlscfg

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"slices"
	"testing"
)

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	c, err := Certificate("127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]})
}

// Ablation checks: each surviving mechanism must prove it earns its place.

// The dev certificate must be usable on both sides of a TLS handshake,
// otherwise mTLS examples/tests built on Certificate() fail with
// "x509: certificate specifies an incompatible key usage".
func TestCertificateBothRoles(t *testing.T) {
	c, err := Certificate("localhost")
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageServerAuth) ||
		!slices.Contains(leaf.ExtKeyUsage, x509.ExtKeyUsageClientAuth) {
		t.Fatalf("expected ServerAuth+ClientAuth, got %v", leaf.ExtKeyUsage)
	}
}

func TestNewDefaults(t *testing.T) {
	cfg, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("Expected MinVersion TLS 1.2, got %#x", cfg.MinVersion)
	}
	// cipher suites stay on the Go runtime's curated defaults
	if len(cfg.CipherSuites) != 0 {
		t.Fatalf("Expected no pinned CipherSuites, got %v", cfg.CipherSuites)
	}
}

func TestOptionsApplyInOrder(t *testing.T) {
	cfg, err := New(WithServerName("a"), WithServerName("b"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ServerName != "b" { // later option wins
		t.Fatalf("Expected ServerName %q, got %q", "b", cfg.ServerName)
	}
}

func TestWithCAAppendsToSystemPool(t *testing.T) {
	sys, err := x509.SystemCertPool()
	if err != nil {
		t.Fatal(err)
	}

	ca := testCAPEM(t)

	cfg, err := New(WithSystemCertPool(), WithCA(ca, ForClient))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs == nil || cfg.RootCAs.Equal(sys) {
		t.Fatal("Expected the custom CA to be appended to the system pool")
	}

	// without WithSystemCertPool the pool is fresh (custom CA only)
	cfg, err = New(WithCA(ca, ForClient))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs.Equal(sys) {
		t.Fatal("Expected a fresh pool, got the system pool")
	}
}

func TestWithCAForServer(t *testing.T) {
	cfg, err := New(WithCA(testCAPEM(t), ForServer))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ClientCAs == nil || cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("Expected mTLS server config, got %+v", cfg)
	}
}

func TestMaybeOptionsNoOp(t *testing.T) {
	cfg, err := New(MaybeWithDiskCA("", ForClient), MaybeWithDiskKeyPair("", ""))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RootCAs != nil || len(cfg.Certificates) != 0 {
		t.Fatalf("Expected empty config, got %+v", cfg)
	}

	// a half-set pair must still fail loudly
	if _, err := New(MaybeWithDiskKeyPair("cert.pem", "")); err == nil {
		t.Fatal("Expected error for half-set cert/key pair, got nil")
	}
}

func TestOptionErrors(t *testing.T) {
	if _, err := New(WithCA([]byte("not a pem"), ForClient)); err == nil {
		t.Fatal("Expected error for invalid CA bytes, got nil")
	}
	if _, err := New(WithDiskCA("missing.pem", ForClient)); err == nil {
		t.Fatal("Expected error for unreadable CA file, got nil")
	}
	if _, err := New(WithDiskKeyPair("a.pem", "")); err == nil {
		t.Fatal("Expected error for half-set keypair paths, got nil")
	}
}

func TestConfigStructUsesNewDefaults(t *testing.T) {
	cfg, err := (&Config{Enable: true, Insecure: true}).Config()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.InsecureSkipVerify || cfg.MinVersion != tls.VersionTLS12 {
		t.Fatalf("Expected insecure TLS 1.2+ config, got %+v", cfg)
	}

	if cfg, err := (&Config{Enable: true}).Config(); err != nil || cfg != nil {
		t.Fatalf("Expected nil config without CA/insecure, got (%v, %v)", cfg, err)
	}

	// version conversion runs before any file is read
	if _, err := (&Config{Enable: true, CA: "missing.pem", MinVersion: "9.9"}).Config(); err == nil {
		t.Fatal("Expected version error, got nil")
	}
}
