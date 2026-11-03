package tlscfg

import (
	"crypto/tls"
	"fmt"
)

// TlsVersion maps a version string like "1.2" to its crypto/tls constant
// | TlsVersion 把 "1.2" 这类版本字符串映射为 crypto/tls 的对应常量.
var TlsVersion = map[string]uint16{
	"1.0": tls.VersionTLS10,
	"1.1": tls.VersionTLS11,
	"1.2": tls.VersionTLS12,
	"1.3": tls.VersionTLS13,
}

func convertVersion(version string, defaultVersion uint16) (uint16, error) {
	if version == "" {
		return defaultVersion, nil
	}
	val, ok := TlsVersion[version]
	if !ok {
		return 0, fmt.Errorf("unsupported TLS version: %q", version)
	}
	return val, nil
}
