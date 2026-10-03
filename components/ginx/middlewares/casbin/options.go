package casbin

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// MatchAllRule requires every item. An empty list skips authorization.
	MatchAllRule ValidationRule = iota
	// AtLeastOneRule requires one matching item. An empty list also skips authorization.
	AtLeastOneRule
)

type (
	// ValidationRule controls how a list of permissions or roles is matched.
	ValidationRule int

	// PermissionParserFunc turns a permission into Casbin request fields after the
	// subject. The default splits "blog:create" into object and action. It runs on
	// each checked item and must be concurrency-safe if handlers share it.
	PermissionParserFunc func(string) []string

	// Option configures a permission or role handler.
	Option interface {
		apply(*Options)
	}

	// OptionFunc adapts a configuration function to Option.
	OptionFunc func(*Options)

	// Options configure one generated handler, independently of the Builder.
	Options struct {
		validationRule   ValidationRule
		permissionParser PermissionParserFunc
	}
)

func (f OptionFunc) apply(o *Options) {
	f(o)
}

// NewOptions creates default options and applies opts in order.
func NewOptions(opts ...Option) *Options {
	options := DefaultOptions()
	options.Apply(opts...)
	return options
}

// DefaultOptions returns an independent default configuration.
func DefaultOptions() *Options {
	return &Options{
		validationRule:   MatchAllRule,
		permissionParser: PermissionParserWithSeparator(":"),
	}
}

// Apply applies opts in order without resetting existing configuration or validating.
// The receiver and options must be valid. Apply must not run concurrently with
// other reads or writes to the same Options.
func (o *Options) Apply(opts ...Option) {
	for _, opt := range opts {
		opt.apply(o)
	}
}

// Validate checks the final configuration without modifying it.
func (o *Options) Validate() error {
	if o.validationRule != MatchAllRule && o.validationRule != AtLeastOneRule {
		return fmt.Errorf("casbin middleware: invalid ValidationRule %d", o.validationRule)
	}
	if o.permissionParser == nil {
		return errors.New("casbin middleware: nil PermissionParser")
	}
	return nil
}

// Apply creates default options and applies opts in order.
// Deprecated: use NewOptions to create options or (*Options).Apply to update them.
func Apply(opts ...Option) *Options {
	return NewOptions(opts...)
}

// WithValidationRule selects all or any matching for one handler.
func WithValidationRule(rule ValidationRule) Option {
	return OptionFunc(func(o *Options) { o.validationRule = rule })
}

// WithPermissionParser sets parsing for permission checks; roles do not invoke it.
func WithPermissionParser(parser PermissionParserFunc) Option {
	return OptionFunc(func(o *Options) { o.permissionParser = parser })
}

// PermissionParserWithSeparator uses strings.Split, including empty fields.
func PermissionParserWithSeparator(separator string) PermissionParserFunc {
	return func(permission string) []string { return strings.Split(permission, separator) }
}
