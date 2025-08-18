package options

import "github.com/spf13/pflag"

// Option defines methods to implement a generic option.
type Option interface {
	// Validate validates all the required option.
	// It can also used to complete option if needed.
	Validate() []error

	// AddFlags adds flags related to given flagset.
	AddFlags(fs *pflag.FlagSet, prefixes ...string)
}
