// Package crawlerdetect rejects requests that claim to come from a known
// search-engine crawler but whose IP does not verify by DNS.
// crawlerdetect 拒绝自称来自已知搜索引擎爬虫、但 IP 无法通过 DNS 验证的请求.
//
// # Notes | 说明
//
// The middleware matches the User-Agent against a configurable prefix map and
// then verifies the client IP with reverse DNS. Baidu, Bing and Google also
// require the forward lookup of the matched host to contain the same IP;
// Sogou only checks the reverse name. Non-crawler User-Agents, unverifiable
// IPs and empty client IPs get 403; DNS failures get 500. Use SetResolver to
// replace the system resolver in tests or custom DNS setups.
// 中间件先用可配置的前缀表匹配 User-Agent，再用反向 DNS 校验客户端 IP。
// 百度、必应、谷歌还要求匹配到的主机正向解析包含同一 IP；搜狗只校验反向
// 名称。非爬虫 UA、无法验证的 IP 和空客户端 IP 返回 403，DNS 失败返回 500。
// 测试或自定义 DNS 场景可用 SetResolver 替换系统解析器。
//
// # Example | 示例
//
//	r.Use(crawlerdetect.NewBuilder().Build())
package crawlerdetect

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// Crawler names accepted by AddUserAgent | AddUserAgent 接受的爬虫名称。
const (
	Baidu  = "baidu"
	Bing   = "bing"
	Google = "google"
	Sogou  = "sogou"
)

// Resolver is the DNS surface used to verify a crawler IP. net.DefaultResolver
// satisfies it; tests and custom DNS setups can replace it.
// Resolver 是校验爬虫 IP 所需的 DNS 接口。net.DefaultResolver 满足它；
// 测试与自定义 DNS 场景可以替换。
type Resolver interface {
	LookupAddr(ctx context.Context, ip string) ([]string, error)
	LookupIP(ctx context.Context, host string) ([]net.IP, error)
}

// systemResolver adapts net.DefaultResolver to Resolver.
// systemResolver 把 net.DefaultResolver 适配为 Resolver。
type systemResolver struct{}

func (systemResolver) LookupAddr(ctx context.Context, ip string) ([]string, error) {
	return net.DefaultResolver.LookupAddr(ctx, ip)
}

func (systemResolver) LookupIP(ctx context.Context, host string) ([]net.IP, error) {
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

type crawler struct {
	hosts        []string
	forwardCheck bool
}

var crawlers = map[string]crawler{
	Baidu:  {hosts: []string{"baidu.com", "baidu.jp"}, forwardCheck: true},
	Bing:   {hosts: []string{"search.msn.com"}, forwardCheck: true},
	Google: {hosts: []string{"googlebot.com", "google.com", "googleusercontent.com"}, forwardCheck: true},
	Sogou:  {hosts: []string{"sogou.com"}},
}

// Builder assembles the crawler-detection middleware | Builder 组装爬虫检测中间件.
type Builder struct {
	userAgents map[string]string
	resolver   Resolver
}

// NewBuilder creates a Builder with the common crawler User-Agents
// | NewBuilder 创建带常用爬虫 UA 映射的 Builder。
func NewBuilder() *Builder {
	return &Builder{
		userAgents: map[string]string{
			"Baiduspider":        Baidu,
			"Baiduspider-render": Baidu,

			"bingbot":          Bing,
			"adidxbot":         Bing,
			"MicrosoftPreview": Bing,

			"Googlebot":             Google,
			"Googlebot-Image":       Google,
			"Googlebot-News":        Google,
			"Googlebot-Video":       Google,
			"Storebot-Google":       Google,
			"Google-InspectionTool": Google,
			"GoogleOther":           Google,
			"Google-Extended":       Google,

			"Sogou web spider": Sogou,
		},
		resolver: systemResolver{},
	}
}

// SetResolver replaces the DNS resolver; nil keeps the current one
// | SetResolver 替换 DNS 解析器；传 nil 保持当前解析器。
func (b *Builder) SetResolver(r Resolver) *Builder {
	if r != nil {
		b.resolver = r
	}
	return b
}

// AddUserAgent maps additional User-Agent prefixes to crawler names
// | AddUserAgent 追加 User-Agent 前缀到爬虫名称的映射。
func (b *Builder) AddUserAgent(userAgents map[string][]string) *Builder {
	for name, prefixes := range userAgents {
		for _, prefix := range prefixes {
			b.userAgents[prefix] = name
		}
	}
	return b
}

// RemoveUserAgent drops User-Agent prefixes from the map
// | RemoveUserAgent 从映射中删除 User-Agent 前缀。
func (b *Builder) RemoveUserAgent(userAgents ...string) *Builder {
	for _, userAgent := range userAgents {
		delete(b.userAgents, userAgent)
	}
	return b
}

// Build returns the middleware | Build 返回中间件。
func (b *Builder) Build() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		ip := ctx.ClientIP()
		if ip == "" {
			slog.ErrorContext(ctx, "crawlerdetect", "error", "ip is empty")
			ctx.AbortWithStatus(http.StatusForbidden)
			return
		}

		name, ok := b.matchUserAgent(ctx.GetHeader("User-Agent"))
		if !ok {
			ctx.AbortWithStatus(http.StatusForbidden)
			return
		}

		pass, err := crawlers[name].check(ctx.Request.Context(), b.resolver, ip)
		if err != nil {
			slog.ErrorContext(ctx, "crawlerdetect", "error", err)
			ctx.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		if !pass {
			ctx.AbortWithStatus(http.StatusForbidden)
			return
		}
		ctx.Next()
	}
}

func (b *Builder) matchUserAgent(userAgent string) (string, bool) {
	for prefix, name := range b.userAgents {
		if strings.Contains(userAgent, prefix) {
			return name, true
		}
	}
	return "", false
}

func (c crawler) check(ctx context.Context, r Resolver, ip string) (bool, error) {
	names, err := r.LookupAddr(ctx, ip)
	if err != nil {
		return false, err
	}
	host, ok := matchHost(c.hosts, names)
	if !ok {
		return false, nil
	}
	if !c.forwardCheck {
		return true, nil
	}

	ips, err := r.LookupIP(ctx, host)
	if err != nil {
		return false, err
	}
	for _, resolved := range ips {
		if resolved.String() == ip {
			return true, nil
		}
	}
	return false, nil
}

func matchHost(hosts, names []string) (string, bool) {
	for _, host := range hosts {
		for _, name := range names {
			if strings.Contains(name, host) {
				return name, true
			}
		}
	}
	return "", false
}
