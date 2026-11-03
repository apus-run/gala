// Package host resolves the address a server should advertise to a
// registry; internal to the server tree | host 解析服务应向注册中心通告的地址，为 server 体系内部包.
//
// # Notes | 说明
//
// [Extract] keeps a configured host as-is (0.0.0.0 and :: are wildcards)
// and swaps in the listener's real port; when the host is a wildcard it
// picks the address of the interface with the lowest index. Internal
// package, not for external use.
// [Extract] 原样保留配置的 host（0.0.0.0 与 :: 视为通配地址），端口换成监听器
// 实际端口；host 为通配地址时按接口序号最小原则从网卡选地址。内部包，不对外使用。
package host

import (
	"fmt"
	"net"
	"strconv"
)

// ExtractHostPort from address | ExtractHostPort 从地址中拆出 host 与端口.
func ExtractHostPort(addr string) (host string, port uint64, err error) {
	var ports string
	host, ports, err = net.SplitHostPort(addr)
	if err != nil {
		return
	}
	port, err = strconv.ParseUint(ports, 10, 16) //nolint:gomnd
	if err != nil {
		return
	}
	return
}

func isValidIP(addr string) bool {
	ip := net.ParseIP(addr)
	return ip.IsGlobalUnicast() && !ip.IsInterfaceLocalMulticast()
}

// Port return a real port | Port 返回监听器的真实端口.
func Port(lis net.Listener) (int, bool) {
	if addr, ok := lis.Addr().(*net.TCPAddr); ok {
		return addr.Port, true
	}
	return 0, false
}

// Extract returns a private addr and port.
// Extract 返回内网地址与端口：配置的 host 有效（非 0.0.0.0/::）时原样使用，
// 否则按接口序号最小原则从网卡地址中选一个.
func Extract(hostPort string, lis net.Listener) (string, error) {
	addr, port, err := net.SplitHostPort(hostPort)
	if err != nil && lis == nil {
		return "", err
	}
	if lis != nil {
		if p, ok := Port(lis); ok {
			port = strconv.Itoa(p)
		} else {
			return "", fmt.Errorf("failed to extract port: %v", lis.Addr())
		}
	}
	if len(addr) > 0 && (addr != "0.0.0.0" && addr != "[::]" && addr != "::") {
		return net.JoinHostPort(addr, port), nil
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	lowest := int(^uint(0) >> 1)
	var result net.IP
	for _, iface := range ifaces {
		if (iface.Flags & net.FlagUp) == 0 {
			continue
		}
		if iface.Index < lowest || result == nil {
			lowest = iface.Index
		} else if result != nil {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, rawAddr := range addrs {
			var ip net.IP
			switch addr := rawAddr.(type) {
			case *net.IPAddr:
				ip = addr.IP
			case *net.IPNet:
				ip = addr.IP
			default:
				continue
			}
			if isValidIP(ip.String()) {
				result = ip
			}
		}
	}
	if result != nil {
		return net.JoinHostPort(result.String(), port), nil
	}
	return "", nil
}
