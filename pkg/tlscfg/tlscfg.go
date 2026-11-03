// Package tlscfg builds *tls.Config values from options, with secure defaults
// | tlscfg 以选项方式构建带安全默认值的 *tls.Config.
//
// # Notes | 说明
//
// New starts every config at a TLS 1.2 minimum and leaves cipher suites and
// curves to the Go runtime defaults. Options apply in order; a later option
// wins when two touch the same field. A CA option starts a fresh pool (system
// certs are dropped) unless WithSystemCertPool ran first; a server-side CA
// also sets RequireAndVerifyClientCert. The struct-based Config shares the
// same defaults and loading: disabled returns (nil, nil), cert/key set without
// a CA is reported as a config error, Insecure sets InsecureSkipVerify.
// Certificate mints a throwaway self-signed pair (ECDSA P-256, valid one year,
// usable for both server and client auth) for local development and tests only.
// New 产出的配置最低版本为 TLS 1.2，密码套件与曲线沿用 Go 运行时默认值。选项按
// 传入顺序生效，两个选项写同一字段时后写的生效。CA 选项会新建证书池（系统证书被
// 丢弃），除非先应用 WithSystemCertPool；服务端 CA 还会设置
// RequireAndVerifyClientCert。结构体 Config 共享同一套默认值与加载逻辑：未启用时
// 返回 (nil, nil)；配了 cert/key 却没有 CA 属于配置错误；Insecure 设置
// InsecureSkipVerify。Certificate 签发一次性自签证书对（ECDSA P-256，有效期一年，
// 同时可用于服务端与客户端认证），仅用于本地开发与测试。
//
// # Example | 示例
//
//	cfg, err := tlscfg.New(
//		tlscfg.WithSystemCertPool(), // system certs, plus the custom CA below
//		tlscfg.MaybeWithDiskCA(*flagCA, tlscfg.ForClient), // no-op when empty
//		tlscfg.WithDiskKeyPair("cert.pem", "key.pem"),
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

// Opt configures a *tls.Config | Opt 配置一个 *tls.Config.
type Opt func(*tls.Config) error

// ForKind specifies whether an option applies to the server or the client
// portion of a *tls.Config (e.g. which CA pool a CA is added to).
//
// ForKind 指定一个选项作用于 *tls.Config 的服务端部分还是客户端部分
// （例如 CA 加入哪个证书池）。
type ForKind uint8

const (
	// ForServer specifies that an option should work on server portions
	// of a *tls.Config (adding a CA, setting RequireAndVerifyClientCert).
	//
	// ForServer 指定选项作用于 *tls.Config 的服务端部分
	// （添加 CA、设置 RequireAndVerifyClientCert）。
	ForServer ForKind = iota
	// ForClient specifies that an option should work on client portions
	// of a *tls.Config (adding a CA).
	//
	// ForClient 指定选项作用于 *tls.Config 的客户端部分（添加 CA）。
	ForClient
)

// New creates and returns a *tls.Config with any options applied in the
// order given. This function will not error if no options are specified.
//
// New 创建并返回一个按给定顺序应用了所有选项的 *tls.Config。
// 未指定任何选项时该函数也不会报错。
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
//
// WithKeyPair 解析 PEM 编码的证书与私钥，并把该密钥对加入 *tls.Config 的
// Certificates。
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
//
// WithDiskKeyPair 从 certPath 与 keyPath 加载 PEM 编码的证书与私钥，
// 并把该密钥对加入 *tls.Config 的 Certificates。
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
// the pair to the *tls.Config's Certificates. If both certPath and keyPath
// are empty, this option does nothing. This option is useful if accepting
// flags to optionally setup a cert.
//
// MaybeWithDiskKeyPair 可选地加载 PEM 编码的证书与私钥，并把该密钥对加入
// *tls.Config 的 Certificates。certPath 与 keyPath 均为空时该选项不做任何事，
// 适用于通过旗帜可选地配置证书的场景。
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
// case also sets RequireAndVerifyClientCert). A nil pool starts fresh, so
// system certs are not included by default; pass WithSystemCertPool first
// to keep them.
//
// WithCA 解析 PEM 编码的 CA 证书，并将其追加到由 forKind 选定的证书池
// （客户端用 RootCAs，服务端用 ClientCAs，服务端场景还会设置
// RequireAndVerifyClientCert）。池为 nil 时会新建池，因此默认不含系统证书；
// 要保留系统证书需先应用 WithSystemCertPool。
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
//
// WithDiskCA 从磁盘加载 PEM 编码的 CA 证书，并加入由 forKind 选定的证书池
// （见 WithCA）。
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
// it to the CA pool selected by forKind (see WithCA). If the path is empty,
// this option does nothing. This option is useful if accepting flags to
// optionally setup a cert.
//
// MaybeWithDiskCA 可选地从磁盘加载 PEM 编码的 CA 证书，并加入由 forKind
// 选定的证书池（见 WithCA）。path 为空时该选项不做任何事，适用于通过旗帜
// 可选地配置证书的场景。
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
// system certs instead of replacing them. Apply it before any WithCA/
// WithDiskCA option. It is necessary only when mixing public-CA and
// custom-CA verification (e.g. while migrating from custom certs to public
// CA certs).
//
// WithSystemCertPool 用系统证书库填充客户端与服务端证书池，使之后应用的
// CA 选项是向系统证书追加而不是替换。需在任何 WithCA/WithDiskCA 选项之前
// 应用。仅在混合公共 CA 与自定义 CA 校验时才需要（例如从自定义证书迁移到
// 公共 CA 证书的过渡期）。
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
//
// WithServerName 设置 *tls.Config 的 ServerName，客户端校验服务端身份时
// 该字段很重要。
func WithServerName(name string) Opt {
	return func(cfg *tls.Config) error {
		cfg.ServerName = name
		return nil
	}
}

// The defaults should be a safe configuration | 默认值应是安全配置.
const defaultMinTLSVersion = tls.VersionTLS12

// Uses the default MaxVersion from "crypto/tls" | 沿用 "crypto/tls" 的默认 MaxVersion.
const defaultMaxTLSVersion = 0

// Config is the configuration for TLS files | Config 是 TLS 文件的配置.
type Config struct {
	// Enable TLS | 是否启用 TLS.
	Enable bool

	// the CA file | CA 文件.
	CA string
	// the cert file | 证书文件.
	Cert string
	// the key file | 私钥文件.
	Key string

	// whether to skip the TLS verification | 是否跳过 TLS 校验.
	Insecure bool

	// MinVersion sets the minimum TLS version that is acceptable.
	// If not set, TLS 1.2 will be used. (optional)
	//
	// MinVersion 设置可接受的最小 TLS 版本；未设置时使用 TLS 1.2。（可选）
	MinVersion string
	// MaxVersion sets the maximum TLS version that is acceptable.
	// If not set, refer to crypto/tls for defaults. (optional)
	//
	// MaxVersion 设置可接受的最大 TLS 版本；未设置时默认值见 crypto/tls。（可选）
	MaxVersion string
}

// Config returns a *tls.Config assembled through New, so the struct-based
// and option-based APIs of this package share the same safe defaults
// (TLS 1.2 minimum) and the same CA/keypair loading.
//
// Config 返回经由 New 组装的 *tls.Config，因此本包的结构体式与选项式 API
// 共享同一套安全默认值（最低 TLS 1.2）与同样的 CA/密钥对加载逻辑。
func (t *Config) Config() (*tls.Config, error) {
	if !t.Enable {
		return nil, nil
	}

	if t.CA == "" {
		if !t.Insecure {
			// No CA and no skip: by convention return nil (no TLS). But a
			// configured cert or key being silently dropped would leave the
			// caller believing TLS is on while traffic is plaintext — that
			// is a config error and must be reported.
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
	// WithDiskKeyPair 会拒绝只配一半的密钥对，而不是静默跳过
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
//
// Certificate 为给定主机返回一张自签证书，适用于本地开发与测试。
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
		// Both roles so a pair of self-signed certs is enough for mTLS
		// tests (Go servers reject client certs without ExtKeyUsageClientAuth).
		// 同时授予两种用途，一张自签证书对就够 mTLS 测试用
		// （Go 服务端会拒绝缺少 ExtKeyUsageClientAuth 的客户端证书）
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
	// 生成公钥
	certOut := bytes.NewBuffer(nil)
	pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	// create private key
	// 生成私钥
	keyOut := bytes.NewBuffer(nil)
	b, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, err
	}
	pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: b})

	return tls.X509KeyPair(certOut.Bytes(), keyOut.Bytes())
}
