// Package tlscfg provides an option-driven way to build a well configured
// *tls.Config with secure defaults.
//
// New returns a config with a TLS 1.2 minimum version and otherwise relies on
// the Go runtime's curated cipher-suite and curve defaults.
//
// Options apply in the order given; a later option wins when two options
// touch the same field.
//
// Usage:
//
//	cfg, err := tlscfg.New(
//	        tlscfg.WithSystemCertPool(), // system certs, plus the custom CA below
//	        tlscfg.MaybeWithDiskCA( // optional CA (no-op when the flag is empty)
//	                *flagCA,
//	                tlscfg.ForClient,
//	        ),
//	        tlscfg.WithDiskKeyPair( // required client cert+key pair
//	                "cert.pem",
//	                "key.pem",
//	        ),
//	)
package tlscfg

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// Opt configures a *tls.Config.
type Opt func(*tls.Config) error

// ForKind specifies whether an option applies to the server or the client
// portion of a *tls.Config (e.g. which CA pool a CA is added to).
type ForKind uint8

const (
	// ForServer specifies that an option should work on server portions
	// of a *tls.Config (adding a CA, setting RequireAndVerifyClientCert).
	ForServer ForKind = iota
	// ForClient specifies that an option should work on client portions
	// of a *tls.Config (adding a CA).
	ForClient
)

// New creates and returns a *tls.Config with any options applied in the
// order given.
//
// This function will not error if no options are specified.
func New(opts ...Opt) (*tls.Config, error) {
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}

	for _, opt := range opts {
		if err := opt(cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// WithKeyPair parses a PEM encoded cert and key and adds the pair to the
// *tls.Config's Certificates.
func WithKeyPair(cert, key []byte) Opt {
	return func(cfg *tls.Config) error {
		pair, err := tls.X509KeyPair(cert, key)
		if err != nil {
			return fmt.Errorf("unable to load keypair: %w", err)
		}
		cfg.Certificates = append(cfg.Certificates, pair)
		return nil
	}
}

// WithDiskKeyPair loads a PEM encoded cert and key from certPath and keyPath
// and adds the pair to the *tls.Config's Certificates.
func WithDiskKeyPair(certPath, keyPath string) Opt {
	return func(cfg *tls.Config) error {
		if certPath == "" || keyPath == "" {
			return errors.New("both cert and key paths must be specified")
		}
		cert, err := os.ReadFile(certPath)
		if err != nil {
			return fmt.Errorf("unable to read cert at %q: %w", certPath, err)
		}
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return fmt.Errorf("unable to read key at %q: %w", keyPath, err)
		}
		return WithKeyPair(cert, key)(cfg)
	}
}

// MaybeWithDiskKeyPair optionally loads a PEM encoded cert and key and adds
// the pair to the *tls.Config's Certificates.
//
// If both certPath and keyPath are empty, this option does nothing. This
// option is useful if accepting flags to optionally setup a cert.
func MaybeWithDiskKeyPair(certPath, keyPath string) Opt {
	return func(cfg *tls.Config) error {
		if certPath == "" && keyPath == "" {
			return nil
		}
		return WithDiskKeyPair(certPath, keyPath)(cfg)
	}
}

// WithCA parses a PEM encoded CA cert and appends it to the CA pool selected
// by forKind (RootCAs for clients, ClientCAs for servers, and the server
// case also sets RequireAndVerifyClientCert).
//
// A nil pool starts fresh, so system certs are not included by default; pass
// WithSystemCertPool first to keep them.
func WithCA(ca []byte, forKind ForKind) Opt {
	return func(cfg *tls.Config) error {
		pool := &cfg.RootCAs
		if forKind == ForServer {
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
			pool = &cfg.ClientCAs
		}

		if *pool == nil {
			*pool = x509.NewCertPool()
		}

		if !(*pool).AppendCertsFromPEM(ca) {
			return errors.New("no cert could be found in the ca bytes")
		}
		return nil
	}
}

// WithDiskCA loads a PEM encoded CA cert from disk and adds it to the CA pool
// selected by forKind (see WithCA).
func WithDiskCA(path string, forKind ForKind) Opt {
	return func(cfg *tls.Config) error {
		if path == "" {
			return errors.New("ca path must be specified")
		}
		ca, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("unable to read ca at %q: %w", path, err)
		}
		return WithCA(ca, forKind)(cfg)
	}
}

// MaybeWithDiskCA optionally loads a PEM encoded CA cert from disk and adds
// it to the CA pool selected by forKind (see WithCA).
//
// If the path is empty, this option does nothing. This option is useful if
// accepting flags to optionally setup a cert.
func MaybeWithDiskCA(path string, forKind ForKind) Opt {
	return func(cfg *tls.Config) error {
		if path == "" {
			return nil
		}
		return WithDiskCA(path, forKind)(cfg)
	}
}

// WithSystemCertPool fills the client and server CA pools from the system
// certificate store, so that CA options applied afterwards append to the
// system certs instead of replacing them.
//
// Apply it before any WithCA/WithDiskCA option. It is necessary only when
// mixing public-CA and custom-CA verification (e.g. while migrating from
// custom certs to public CA certs).
func WithSystemCertPool() Opt {
	return func(cfg *tls.Config) error {
		for _, pool := range []**x509.CertPool{&cfg.RootCAs, &cfg.ClientCAs} {
			if *pool == nil {
				p, err := x509.SystemCertPool()
				if err != nil {
					return fmt.Errorf("unable to load system cert pool: %w", err)
				}
				*pool = p
			}
		}
		return nil
	}
}

// WithServerName sets the *tls.Config's ServerName, which is important for
// clients to verify servers.
func WithServerName(name string) Opt {
	return func(cfg *tls.Config) error {
		cfg.ServerName = name
		return nil
	}
}

// The defaults should be a safe configuration
const defaultMinTLSVersion = tls.VersionTLS12

// Uses the default MaxVersion from "crypto/tls"
const defaultMaxTLSVersion = 0

// Config TLS is the configuration for TLS files
type Config struct {
	// Enable TLS
	Enable bool

	// the CA file
	CA string
	// the cert file
	Cert string
	// the key file
	Key string

	// whether to skip the TLS verification
	Insecure bool

	// MinVersion sets the minimum TLS version that is acceptable.
	// If not set, TLS 1.2 will be used. (optional)
	MinVersion string
	// MaxVersion sets the maximum TLS version that is acceptable.
	// If not set, refer to crypto/tls for defaults. (optional)
	MaxVersion string
}

// Config returns a *tls.Config assembled through New, so the struct-based
// and option-based APIs of this package share the same safe defaults
// (TLS 1.2 minimum) and the same CA/keypair loading.
func (t *Config) Config() (*tls.Config, error) {
	if !t.Enable {
		return nil, nil
	}

	if t.CA == "" {
		if !t.Insecure {
			// 没有 CA 且不跳过校验：按约定返回 nil（不使用 TLS）。但配了证书或私钥却被静默丢弃，
			// 调用方会以为在用 TLS 实际是明文，这是配置错误，必须报出来
			if t.Cert != "" || t.Key != "" {
				return nil, errors.New("tlscfg: cert/key are set but ca is empty; set ca, or clear cert/key to run without TLS")
			}
			return nil, nil
		}
		return New(func(c *tls.Config) error {
			c.InsecureSkipVerify = true
			return nil
		})
	}

	opts := []Opt{WithDiskCA(t.CA, ForClient)}

	// WithDiskKeyPair rejects a half-set pair instead of silently skipping it
	if t.Cert != "" || t.Key != "" {
		opts = append(opts, WithDiskKeyPair(t.Cert, t.Key))
	}

	minV, err := convertVersion(t.MinVersion, defaultMinTLSVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid TLS min_version: %w", err)
	}
	maxV, err := convertVersion(t.MaxVersion, defaultMaxTLSVersion)
	if err != nil {
		return nil, fmt.Errorf("invalid TLS max_version: %w", err)
	}

	opts = append(opts, func(c *tls.Config) error {
		c.InsecureSkipVerify = t.Insecure
		c.MinVersion = minV
		c.MaxVersion = maxV
		return nil
	})

	return New(opts...)
}

// Certificate returns a self-signed certificate for the given hosts, useful
// for local development and tests.
func Certificate(host ...string) (tls.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}

	notBefore := time.Now()
	notAfter := notBefore.Add(time.Hour * 24 * 365)

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Acme Co"},
		},
		NotBefore: notBefore,
		NotAfter:  notAfter,

		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		// both roles so a pair of self-signed certs is enough for mTLS tests
		// (Go servers reject client certs without ExtKeyUsageClientAuth)
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	for _, h := range host {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	template.IsCA = true
	template.KeyUsage |= x509.KeyUsageCertSign

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	// create public key
	certOut := bytes.NewBuffer(nil)
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	// create private key
	keyOut := bytes.NewBuffer(nil)
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})

	return tls.X509KeyPair(certOut.Bytes(), keyOut.Bytes())
}
