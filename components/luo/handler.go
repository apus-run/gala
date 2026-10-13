package luo

import "net/http"

// HandlerFunc is an optional error-returning HTTP handler. Use Adapt to register
// it with a Group or any net/http router. Existing route methods continue to
// accept standard http.HandlerFunc values.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// ErrorHandler handles a returned error. It owns logging, status mapping and
// response formatting; luo does not depend on a particular error type or codec.
type ErrorHandler func(http.ResponseWriter, *http.Request, error)

// Adapt converts an error-returning handler to a standard http.HandlerFunc.
// Both arguments must be non-nil. The original writer and request are preserved,
// including streaming/hijacking interfaces. The handler should return errors
// before writing a response; otherwise onError must avoid writing it again.
func Adapt(handler HandlerFunc, onError ErrorHandler) http.HandlerFunc {
	if handler == nil || onError == nil {
		panic("luo: Adapt requires a handler and an error handler")
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			onError(w, r, err)
		}
	}
}
