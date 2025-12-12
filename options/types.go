package options

import "github.com/spf13/pflag"

// Option defines methods to implement a generic option.
type Option interface {
	// Validate validates all the required option.
	// It can also be used to complete option if needed.
	// If there are multiple errors, it should return a single error that
	// contains all the errors. For example, using errors.Join from Go 1.20:
	// errs := []error
	// if err := o.Validate(); err != nil {
	//    errs = append(errs, err)
	// }
	// if err := o.Complete(); err != nil {
	//    errs = append(errs, err)
	// }
	// if len(errs) > 0 {
	// 	  return errors.Join(errs...)
	// }
	// If the option is not valid, it should return an error.
	// If the option is valid, it should return nil.
	Validate() error

	// AddFlags adds flags related to given flag.
	AddFlags(fs *pflag.FlagSet, prefixes ...string)
}
