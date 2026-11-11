package conf

import "strconv"

// Stage identifies the failed step of Open or Reload.
type Stage string

const (
	StageConfig   Stage = "config"   // Invalid settings or target type.
	StageLoad     Stage = "load"     // A source returned an error.
	StageMerge    Stage = "merge"    // Invalid source data or expansion limit.
	StageResolve  Stage = "resolve"  // Placeholder expansion or custom resolution failed.
	StageBind     Stage = "bind"     // A field could not be bound.
	StageValidate Stage = "validate" // Business validation failed.
	StageCanceled Stage = "canceled" // Canceled or timed out.
)

// Error describes a failed operation. Source is zero-based, or -1 when
// unknown. Path includes only safe, statically declared field names.
type Error struct {
	Stage  Stage
	Source int
	Path   string
	Err    error
}

// Error includes the source's own diagnostic for StageLoad. Business
// validation errors are exposed through Unwrap and have fixed display text.
func (e *Error) Error() string {
	msg := "conf: " + string(e.Stage) + " failed"
	if e.Source >= 0 {
		msg += " (source " + strconv.Itoa(e.Source) + ")"
	}
	if e.Path != "" {
		msg += " at " + e.Path
	}
	if e.Stage == StageValidate {
		return msg + ": validation rejected configuration"
	}
	if e.Err != nil {
		return msg + ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is and errors.As.
func (e *Error) Unwrap() error { return e.Err }
