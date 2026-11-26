// CLI trigger adapter: parses flags and maps them to one Registry.Run call.
// CLI 触发适配器：解析命令行参数并映射为一次 Registry.Run 调用。

package job

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RunCLI triggers one job from command-line style arguments.
//
// Flags: --job names the registered job (required), --job-data is the payload
// ("-" reads stdin when stdin is non-nil; empty means no payload), and
// --job-header is a semicolon-separated list of key=value pairs, each split on
// its first "=" and added to Invocation.Header. The FlagSet is named "job" and
// its output is discarded: RunCLI never prints, exits or logs, it returns
// errors for the caller to report.
// RunCLI 以命令行风格的参数触发一次任务。
//
// 参数：--job 指定已注册的任务（必填），--job-data 是 payload（"-" 表示在
// stdin 非 nil 时读取 stdin；空值表示无 payload），--job-header 是分号分隔的
// key=value 列表，每项在第一个 "=" 处拆分并加入 Invocation.Header。FlagSet 名为
// "job" 且输出被丢弃：RunCLI 不打印、不退出、不记录日志，而是返回错误由调用方
// 处理。
func (r *Registry) RunCLI(ctx context.Context, args []string, stdin io.Reader) error {
	fs := flag.NewFlagSet("job", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	var (
		name    string
		data    string
		headers string
	)
	fs.StringVar(&name, "job", "", "name of the registered job to run")
	fs.StringVar(&data, "job-data", "", `payload for the job; "-" reads stdin`)
	fs.StringVar(&headers, "job-header", "", "semicolon-separated key=value invocation headers")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if name == "" {
		return fmt.Errorf("%w: --job is required", ErrEmptyName)
	}

	// Reject an unknown job before reading stdin: a typo must not consume (or
	// buffer) the piped input only to fail with ErrNotFound.
	// 读取 stdin 之前先拒绝未知任务：拼错任务名不应先消费（并缓冲）管道输入再
	// 返回 ErrNotFound。
	if !r.has(name) {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}

	inv := Invocation{Name: name}

	switch {
	case data == "-":
		if stdin != nil {
			// Cap the payload like the HTTP adapter does, so a large stream
			// cannot exhaust memory.
			// 与 HTTP 适配器一样限制 payload 大小，避免大输入耗尽内存。
			limited := io.LimitReader(stdin, defaultMaxBodyBytes+1)
			payload, err := io.ReadAll(limited)
			if err != nil {
				return fmt.Errorf("job: read stdin: %w", err)
			}
			if int64(len(payload)) > defaultMaxBodyBytes {
				return fmt.Errorf("job: stdin payload exceeds %d bytes", defaultMaxBodyBytes)
			}
			inv.Payload = payload
		}
	case data != "":
		inv.Payload = []byte(data)
	}

	if headers != "" {
		h, err := parseHeaderFlag(headers)
		if err != nil {
			return err
		}
		inv.Header = h
	}

	return r.Run(ctx, inv)
}

// parseHeaderFlag parses the --job-header value: entries separated by ";",
// each key and value split on the first "=" and whitespace-trimmed. Keys may
// repeat; values may contain "=".
// parseHeaderFlag 解析 --job-header 的值：条目以 ";" 分隔，每项的键与值在第一个
// "=" 处拆分并去除首尾空白。键可重复，值可包含 "="。
func parseHeaderFlag(s string) (http.Header, error) {
	h := make(http.Header)

	for _, entry := range strings.Split(s, ";") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("job: invalid --job-header entry %q: want key=value", entry)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("job: invalid --job-header entry %q: empty key", entry)
		}
		h.Add(key, strings.TrimSpace(value))
	}

	if len(h) == 0 {
		return nil, nil
	}

	return h, nil
}
