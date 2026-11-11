package fileerr

import (
	"errors"
	"io/fs"
)

// OnlyMissing reports whether every cause represents a missing file.
func OnlyMissing(err error) bool {
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		causes := wrapped.Unwrap()
		if len(causes) == 0 {
			return false
		}
		for _, cause := range causes {
			if !OnlyMissing(cause) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		if cause := wrapped.Unwrap(); cause != nil {
			return OnlyMissing(cause)
		}
	}
	return errors.Is(err, fs.ErrNotExist)
}
