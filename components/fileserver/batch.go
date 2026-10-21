package fileserver

import (
	"context"
	"errors"
	"io"
	"sync"
)

const (
	// DefaultBatchConcurrency 是批量传输的默认并发度。
	DefaultBatchConcurrency = 4
	// MaxBatchConcurrency 是批量传输允许的最大并发度。
	MaxBatchConcurrency = 64
)

// UploadTask 是一次批量上传中的一项。Open 在该项真正开始时才调用，避免同时持有全部文件。
// 返回的 ReadCloser 由 UploadAll 关闭。
type UploadTask struct {
	Key  string
	Open func(ctx context.Context) (io.ReadCloser, error)
	Opts []UploadOpt
}

// DownloadTask 是一次批量下载中的一项。W 由调用方拥有，DownloadAll 不关闭它。
type DownloadTask struct {
	Key  string
	W    io.WriterAt
	Opts []DownloadOpt
}

// Result 是批量传输中一项的结果。切片与输入同序、同长。
type Result struct {
	Index int
	Key   string
	Err   error
}

// BatchOpt 配置一次批量调用。
type BatchOpt interface {
	applyBatch(*batchConfig)
}

type batchConfig struct {
	concurrency int
	failFast    bool
}

type batchOptFunc func(*batchConfig)

func (f batchOptFunc) applyBatch(c *batchConfig) { f(c) }

// WithConcurrency 设置同时进行的任务数，默认 DefaultBatchConcurrency，范围 1 到 MaxBatchConcurrency。
func WithConcurrency(n int) BatchOpt {
	return batchOptFunc(func(c *batchConfig) { c.concurrency = n })
}

// WithFailFast 在某一项失败后取消其余项。尚未开始的项返回上下文的取消错误。
func WithFailFast() BatchOpt {
	return batchOptFunc(func(c *batchConfig) { c.failFast = true })
}

// UploadAll 上传全部任务。一项的失败默认不影响其他项。
// 所有工作协程在函数返回前退出。
func UploadAll(ctx context.Context, u Uploader, tasks []UploadTask, opts ...BatchOpt) []Result {
	keys := make([]string, len(tasks))
	for i := range tasks {
		keys[i] = tasks[i].Key
	}
	if u == nil {
		return failedResults(keys, invalidArgument("uploader is nil"))
	}
	return runBatch(ctx, keys, opts, func(ctx context.Context, i int) error {
		return runUpload(ctx, u, tasks[i])
	})
}

// DownloadAll 下载全部任务。一项的失败默认不影响其他项。
// 所有工作协程在函数返回前退出。W 不会被关闭。
func DownloadAll(ctx context.Context, d Downloader, tasks []DownloadTask, opts ...BatchOpt) []Result {
	keys := make([]string, len(tasks))
	for i := range tasks {
		keys[i] = tasks[i].Key
	}
	if d == nil {
		return failedResults(keys, invalidArgument("downloader is nil"))
	}
	return runBatch(ctx, keys, opts, func(ctx context.Context, i int) error {
		return runDownload(ctx, d, tasks[i])
	})
}

func runUpload(ctx context.Context, u Uploader, task UploadTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.Open == nil {
		return invalidArgument("upload task has no opener")
	}
	r, err := task.Open(ctx)
	if err != nil {
		return err
	}
	if r == nil {
		return invalidArgument("upload task opener returned a nil reader")
	}
	err = u.Upload(ctx, task.Key, r, task.Opts...)
	return errors.Join(err, r.Close())
}

func runDownload(ctx context.Context, d Downloader, task DownloadTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if task.W == nil {
		return invalidArgument("download task has no writer")
	}
	return d.Download(ctx, task.Key, task.W, task.Opts...)
}

func failedResults(keys []string, err error) []Result {
	out := make([]Result, len(keys))
	for i, key := range keys {
		out[i] = Result{Index: i, Key: key, Err: err}
	}
	return out
}

// runBatch 按输入顺序返回结果。work 对每个下标最多调用一次，并且可以并发调用。
func runBatch(ctx context.Context, keys []string, opts []BatchOpt, work func(context.Context, int) error) []Result {
	results := make([]Result, len(keys))
	for i, key := range keys {
		results[i] = Result{Index: i, Key: key}
	}
	cfg, err := newBatchConfig(opts)
	if err != nil {
		for i := range results {
			results[i].Err = err
		}
		return results
	}
	if len(keys) == 0 {
		return results
	}

	var cancel context.CancelFunc
	if cfg.failFast {
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	sem := make(chan struct{}, cfg.concurrency)
	var wg sync.WaitGroup
	for i := range results {
		select {
		case <-ctx.Done():
			results[i].Err = ctx.Err()
			continue
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			err := work(ctx, i)
			results[i].Err = err
			if err != nil && cfg.failFast {
				cancel()
			}
		}(i)
	}
	wg.Wait()
	return results
}

func newBatchConfig(opts []BatchOpt) (batchConfig, error) {
	cfg := batchConfig{concurrency: DefaultBatchConcurrency}
	for _, opt := range opts {
		if opt != nil {
			opt.applyBatch(&cfg)
		}
	}
	if cfg.concurrency < 1 || cfg.concurrency > MaxBatchConcurrency {
		return batchConfig{}, invalidArgument("batch concurrency must be between 1 and 64")
	}
	return cfg, nil
}
