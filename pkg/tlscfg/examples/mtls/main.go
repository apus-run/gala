// mtls 演示 tlscfg 的真实用法：磁盘证书文件 + mTLS HTTPS。
//
// 每次运行先把两张自签开发证书写入 testdata/（真实项目里这些文件来自
// 你的 CA / cert-manager），随后双方全部用磁盘版 option——WithDiskKeyPair/
// WithDiskCA——组装 *tls.Config：服务端要求并校验客户端证书，客户端出示
// 自己的证书、信任服务端证书并校验其身份。
//
// 运行（在 pkg/tlscfg 目录下，相对路径 testdata/ 由此解析）：
//
//	go run ./examples/mtls
package main

import (
	"crypto/ecdsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/apus-run/gala/pkg/tlscfg"
)

// pemPair 把 tls.Certificate 编回 PEM
func pemPair(c tls.Certificate) (certPEM, keyPEM []byte, err error) {
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]})

	key, err := x509.MarshalECPrivateKey(c.PrivateKey.(*ecdsa.PrivateKey))
	if err != nil {
		return nil, nil, err
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: key})
	return certPEM, keyPEM, nil
}

// writeCert 把一对 PEM 写到 testdata/ 下，模拟运维放置证书文件
func writeCert(name string, c tls.Certificate) (certPath, keyPath string, err error) {
	certPEM, keyPEM, err := pemPair(c)
	if err != nil {
		return "", "", err
	}

	certPath = filepath.Join("testdata", name+".pem")
	keyPath = filepath.Join("testdata", name+".key")
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return "", "", err
	}
	return certPath, keyPath, nil
}

func main() {
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		panic(err)
	}

	serverCert, err := tlscfg.Certificate("localhost", "127.0.0.1")
	if err != nil {
		panic(err)
	}
	clientCert, err := tlscfg.Certificate()
	if err != nil {
		panic(err)
	}
	sCert, sKey, err := writeCert("server", serverCert)
	if err != nil {
		panic(err)
	}
	cCert, cKey, err := writeCert("client", clientCert)
	if err != nil {
		panic(err)
	}
	fmt.Printf("cert files written: %s, %s, %s, %s\n", sCert, sKey, cCert, cKey)

	// 服务端：出示自己的密钥对；ForServer 把客户端证书写入 ClientCAs
	// 并自动启用 RequireAndVerifyClientCert——没有客户端证书的握手直接被拒
	serverCfg, err := tlscfg.New(
		tlscfg.WithDiskKeyPair(sCert, sKey),
		tlscfg.WithDiskCA(cCert, tlscfg.ForServer),
	)
	if err != nil {
		panic(err)
	}

	// 客户端：出示自己的密钥对；ForClient 把服务端证书写入 RootCAs，
	// WithServerName 告诉它校验谁的身份
	clientCfg, err := tlscfg.New(
		tlscfg.WithServerName("localhost"),
		tlscfg.WithDiskKeyPair(cCert, cKey),
		tlscfg.WithDiskCA(sCert, tlscfg.ForClient),
	)
	if err != nil {
		panic(err)
	}

	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, "hello %s, your client cert verified\n", r.TLS.PeerCertificates[0].Subject.Organization)
		}),
		TLSConfig: serverCfg,
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	go func() { _ = srv.ServeTLS(ln, "", "") }()
	defer srv.Close()

	url := "https://" + ln.Addr().String() + "/"

	// 1. 带证书的客户端：mTLS 握手成功
	authorized := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: clientCfg},
	}
	resp, err := authorized.Get(url)
	if err != nil {
		panic(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	fmt.Printf("authorized client: %s -> %s", resp.Status, body)

	// 2. 不带证书的客户端：只在握手阶段就被拒（根本没有 HTTP 层）
	bareCfg, err := tlscfg.New(
		tlscfg.WithServerName("localhost"),
		tlscfg.WithDiskCA(sCert, tlscfg.ForClient),
	)
	if err != nil {
		panic(err)
	}
	bare := &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{TLSClientConfig: bareCfg},
	}
	if _, err := bare.Get(url); err != nil {
		fmt.Printf("bare client rejected at the handshake: %v\n", err)
	}
}
