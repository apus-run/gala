package consul

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// client is a minimal Consul HTTP API client.
type client struct {
	base       string
	http       *http.Client
	token      string
	datacenter string
	timeout    time.Duration
}

func newClient(o Options) *client {
	return &client{
		base:       o.address,
		http:       o.httpClient,
		token:      o.token,
		datacenter: o.datacenter,
		timeout:    o.requestTimeout,
	}
}

// statusError is a non-2xx answer from Consul.
type statusError struct {
	Method, Path string
	Code         int
	Body         string
}

func (e *statusError) Error() string {
	return fmt.Sprintf("consul: %s %s: %d %s", e.Method, e.Path, e.Code, e.Body)
}

func isNotFound(err error) bool {
	var se *statusError
	return errors.As(err, &se) && se.Code == http.StatusNotFound
}

// isFatal reports an error that retrying cannot fix: a client error such as
// a missing ACL permission. Timeouts and rate limits are not fatal.
func isFatal(err error) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return false
	}
	switch se.Code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return false
	}
	return se.Code >= 400 && se.Code < 500
}

// do sends one request and returns the X-Consul-Index of the answer. A
// non-blocking request is bounded by the client timeout; a blocking query is
// bounded by the server wait plus jitter and a request timeout margin.
func (c *client) do(ctx context.Context, method, path string, query url.Values, in, out any, blocking bool) (uint64, error) {
	timeout := c.timeout
	if blocking {
		wait, _ := time.ParseDuration(query.Get("wait"))
		timeout += wait + wait/16 // Consul may add up to wait/16 of jitter.
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if query == nil {
		query = url.Values{}
	}
	if c.datacenter != "" {
		query.Set("dc", c.datacenter)
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, fmt.Errorf("consul: encode request: %w", err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return 0, err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("X-Consul-Token", c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return 0, &statusError{Method: method, Path: path, Code: resp.StatusCode, Body: string(bytes.TrimSpace(b))}
	}
	var index uint64
	if v := resp.Header.Get("X-Consul-Index"); v != "" {
		index, _ = strconv.ParseUint(v, 10, 64)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return index, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return 0, fmt.Errorf("consul: decode %s response: %w", path, err)
	}
	return index, nil
}
