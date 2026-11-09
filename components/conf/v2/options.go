package conf

import (
	"context"
	"slices"
	"time"
)

// MergeFunc merges src into dst. Both maps are private candidates. Do not retain
// them for concurrent mutation. Returning an error discards the entire batch.
// Input objects are map[string]any; slices and arrays are normalized to []any.
type MergeFunc func(dst, src map[string]any) error

// Resolver transforms the merged candidate before binding. It must honor ctx
// and must not mutate the candidate after returning.
type Resolver func(context.Context, map[string]any) error

// Option configures an instance before its first load.
type Option func(*options)
type options struct {
	sources []Source
	merge   MergeFunc
	resolve Resolver
	watch   bool
	poll    time.Duration
}

// WithSources appends sources in increasing priority order and captures the list.
func WithSources(sources ...Source) Option {
	captured := slices.Clone(sources)
	return func(o *options) { o.sources = append(o.sources, captured...) }
}

// WithMerge replaces the default recursive object merge. Nil uses the default.
func WithMerge(merge MergeFunc) Option { return func(o *options) { o.merge = merge } }

// WithResolver transforms the complete merged candidate before strict binding.
func WithResolver(resolve Resolver) Option { return func(o *options) { o.resolve = resolve } }

// WithWatch starts automatic reload after a successful initial load. The Open
// context owns its lifetime; Close cancels it and waits for all workers.
func WithWatch() Option { return func(o *options) { o.watch = true } }

// WithPollInterval enables periodic full reload alongside any source watchers.
// Zero disables polling. A negative interval is invalid. Call Start or use
// WithWatch to start the configured monitoring.
func WithPollInterval(interval time.Duration) Option { return func(o *options) { o.poll = interval } }

// SourceFunc adapts an ordinary function to Source.
type SourceFunc func(context.Context) (map[string]any, error)

func (f SourceFunc) Load(ctx context.Context) (map[string]any, error) { return f(ctx) }
