// Package consul implements registry.Registry and registry.Discovery on the
// Consul agent HTTP API. It uses only the standard library.
//
// Registration: each instance becomes a Consul service with a TTL health
// check. A background goroutine passes the check every TTL/3; if Consul has
// forgotten the service (agent restart without persistence) it is registered
// again. If the process dies, the check turns critical and the instance
// disappears from discovery; Consul removes it for good after
// WithDeregisterCriticalAfter.
//
// Discovery: only instances whose health checks pass are returned. Subscribe
// uses blocking queries and diffs successive snapshots into Add and Delete
// events. After an outage, discovery converges to the latest snapshot;
// intermediate transitions between snapshots are not replayed.
//
// Mapping: a Consul service has one Address and Port, but a
// registry.ServiceInstance has several endpoints. The first endpoint fills
// Address and Port (for DNS and other Consul clients); the full list, the
// version and the instance ID travel in Meta under the reserved "gala."
// prefix. Services registered by other tools are still discovered, with one
// "tcp://host:port" endpoint.
package consul

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAddress         = "http://127.0.0.1:8500"
	defaultTTL             = 15 * time.Second
	defaultDeregisterAfter = time.Minute
	defaultWait            = 30 * time.Second
	defaultRequestTimeout  = 10 * time.Second
	defaultBufferSize      = 64
	defaultRetryMin        = 500 * time.Millisecond
	defaultRetryMax        = 10 * time.Second
	defaultMinPoll         = 200 * time.Millisecond
)

type options struct {
	address         string
	token           string
	datacenter      string
	ttl             time.Duration
	deregisterAfter time.Duration
	wait            time.Duration
	requestTimeout  time.Duration
	bufferSize      int
	httpClient      *http.Client
	logger          *slog.Logger

	// Unexported so tests can shorten them.
	checkInterval time.Duration // 0 means ttl/3
	retryMin      time.Duration // backoff after a failed poll
	retryMax      time.Duration
	minPoll       time.Duration // floor between polls that return nothing new
}

// Option configures a Registry or a Discovery.
type Option func(*options)

// WithAddress sets the Consul agent address, "host:port" or a full URL.
// Default: $CONSUL_HTTP_ADDR, else http://127.0.0.1:8500.
func WithAddress(addr string) Option { return func(o *options) { o.address = addr } }

// WithToken sets the ACL token. Default: $CONSUL_HTTP_TOKEN.
func WithToken(token string) Option { return func(o *options) { o.token = token } }

// WithDatacenter sets the datacenter to query. Default: the agent's.
func WithDatacenter(dc string) Option { return func(o *options) { o.datacenter = dc } }

// WithTTL sets the TTL of the health check. An instance becomes critical
// this long after its process stops passing the check. Default: 15s.
func WithTTL(ttl time.Duration) Option { return func(o *options) { o.ttl = ttl } }

// WithDeregisterCriticalAfter makes Consul remove an instance whose check
// has been critical this long. Zero disables it. Default: 1m.
func WithDeregisterCriticalAfter(d time.Duration) Option {
	return func(o *options) { o.deregisterAfter = d }
}

// WithWaitTime sets how long a blocking query waits for a change before
// returning. Default: 30s.
func WithWaitTime(d time.Duration) Option { return func(o *options) { o.wait = d } }

// WithRequestTimeout bounds every non-blocking request. Default: 10s.
func WithRequestTimeout(d time.Duration) Option { return func(o *options) { o.requestTimeout = d } }

// WithBufferSize sets how many events a subscription can queue beyond its
// initial replay before it is closed as a slow consumer. Default: 64.
func WithBufferSize(n int) Option { return func(o *options) { o.bufferSize = n } }

// WithHTTPClient sets the HTTP client, e.g. for TLS. Its Timeout must be
// longer than WithWaitTime, or blocking queries will be cut short.
func WithHTTPClient(c *http.Client) Option {
	return func(o *options) {
		if c != nil {
			o.httpClient = c
		}
	}
}

// WithLogger sets the logger. Default: slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(o *options) {
		if l != nil {
			o.logger = l
		}
	}
}

func newOptions(opts []Option) (options, error) {
	o := options{
		address:         os.Getenv("CONSUL_HTTP_ADDR"),
		token:           os.Getenv("CONSUL_HTTP_TOKEN"),
		ttl:             defaultTTL,
		deregisterAfter: defaultDeregisterAfter,
		wait:            defaultWait,
		requestTimeout:  defaultRequestTimeout,
		bufferSize:      defaultBufferSize,
		httpClient:      &http.Client{},
		logger:          slog.Default(),
		retryMin:        defaultRetryMin,
		retryMax:        defaultRetryMax,
		minPoll:         defaultMinPoll,
	}
	for _, opt := range opts {
		opt(&o)
	}

	addr, err := normalizeAddress(o.address)
	if err != nil {
		return options{}, err
	}
	o.address = addr

	switch {
	case o.ttl <= 0:
		o.ttl = defaultTTL
	case o.ttl < time.Second:
		o.ttl = time.Second
	}
	if o.deregisterAfter < 0 {
		o.deregisterAfter = 0
	}
	if o.wait <= 0 {
		o.wait = defaultWait
	}
	if o.requestTimeout <= 0 {
		o.requestTimeout = defaultRequestTimeout
	}
	if o.bufferSize <= 0 {
		o.bufferSize = defaultBufferSize
	}
	return o, nil
}

func normalizeAddress(addr string) (string, error) {
	if addr == "" {
		addr = defaultAddress
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("consul registry: invalid address %q", addr)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// interval is how often the TTL check is passed.
func (o *options) interval() time.Duration {
	if o.checkInterval > 0 {
		return o.checkInterval
	}
	return o.ttl / 3
}
