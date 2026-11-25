package crawlerdetect

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// fakeResolver answers from fixed tables so the tests need no real DNS.
// fakeResolver 用固定表回答，测试不依赖真实 DNS。
type fakeResolver struct {
	names map[string][]string
	ips   map[string][]net.IP
}

func (f fakeResolver) LookupAddr(_ context.Context, ip string) ([]string, error) {
	names, ok := f.names[ip]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: ip, IsNotFound: true}
	}
	return names, nil
}

func (f fakeResolver) LookupIP(_ context.Context, host string) ([]net.IP, error) {
	ips, ok := f.ips[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return ips, nil
}

// baiduDNS maps the Baidu test IP to a verifiable host. Note the test IP is
// outside Baidu's real ranges on purpose: the check is DNS-only.
// baiduDNS 把百度测试 IP 映射到可验证的主机。测试 IP 故意不在百度真实网段内：
// 校验只看 DNS。
var baiduDNS = fakeResolver{
	names: map[string][]string{"111.206.198.69": {"crawl-111-206-198-69.baidu.com."}},
	ips:   map[string][]net.IP{"crawl-111-206-198-69.baidu.com.": {net.ParseIP("111.206.198.69")}},
}

func newRequest(userAgent, ip string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = ""
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if ip != "" {
		req.RemoteAddr = ip + ":1234"
	}
	return req
}

func TestBuilder_Build(t *testing.T) {
	gin.SetMode(gin.TestMode)

	baiduUA := "Mozilla/5.0 (compatible; Baiduspider/2.0; +http://www.baidu.com/search/spider.html)"

	testCases := []struct {
		name      string
		userAgent string
		ip        string
		mutate    func(*Builder)
		resolver  Resolver
		wantCode  int
	}{
		{name: "empty ip", userAgent: baiduUA, wantCode: http.StatusForbidden},
		{
			name:      "plain browser is rejected",
			userAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/89.0 Safari/537.36",
			ip:        "111.206.198.69",
			wantCode:  http.StatusForbidden,
		},
		{
			name:      "baidu verified",
			userAgent: baiduUA,
			ip:        "111.206.198.69",
			resolver:  baiduDNS,
			wantCode:  http.StatusOK,
		},
		{
			name:      "baidu ip does not reverse to baidu",
			userAgent: baiduUA,
			ip:        "166.249.90.77",
			resolver: fakeResolver{
				names: map[string][]string{"166.249.90.77": {"host.example.com."}},
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:      "forward lookup disagrees",
			userAgent: baiduUA,
			ip:        "111.206.198.69",
			resolver: fakeResolver{
				names: baiduDNS.names,
				ips:   map[string][]net.IP{"crawl-111-206-198-69.baidu.com.": {net.ParseIP("10.0.0.1")}},
			},
			wantCode: http.StatusForbidden,
		},
		{
			name:      "dns failure",
			userAgent: baiduUA,
			ip:        "111.206.198.69",
			resolver: fakeResolver{
				names: baiduDNS.names,
				ips:   baiduDNS.ips,
			},
			mutate: func(b *Builder) {
				b.SetResolver(failingResolver{})
			},
			wantCode: http.StatusInternalServerError,
		},
		{
			name:      "sogou needs no forward lookup",
			userAgent: "Sogou web spider/4.0(+http://www.sogou.com/docs/help/webmasters.htm#07)",
			ip:        "1.2.3.4",
			resolver: fakeResolver{
				names: map[string][]string{"1.2.3.4": {"crawl-1-2-3-4.sogou.com."}},
			},
			wantCode: http.StatusOK,
		},
		{
			name:      "custom user-agent added",
			userAgent: "MySpider/1.0",
			ip:        "111.206.198.69",
			mutate: func(b *Builder) {
				b.AddUserAgent(map[string][]string{Baidu: {"MySpider"}})
			},
			resolver: baiduDNS,
			wantCode: http.StatusOK,
		},
		{
			name:      "removed user-agent stops matching",
			userAgent: baiduUA,
			ip:        "111.206.198.69",
			mutate: func(b *Builder) {
				b.RemoveUserAgent("Baiduspider")
			},
			resolver: baiduDNS,
			wantCode: http.StatusForbidden,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder()
			if tc.resolver != nil {
				b.SetResolver(tc.resolver)
			}
			if tc.mutate != nil {
				tc.mutate(b)
			}

			r := gin.New()
			r.Use(b.Build())
			r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

			w := httptest.NewRecorder()
			r.ServeHTTP(w, newRequest(tc.userAgent, tc.ip))
			assert.Equal(t, tc.wantCode, w.Code)
		})
	}
}

type failingResolver struct{}

func (failingResolver) LookupAddr(context.Context, string) ([]string, error) {
	return nil, errors.New("dns down")
}
func (failingResolver) LookupIP(context.Context, string) ([]net.IP, error) {
	return nil, errors.New("dns down")
}

func TestSetResolverIgnoresNil(t *testing.T) {
	b := NewBuilder()
	b.SetResolver(nil)
	assert.NotNil(t, b.resolver)
}
