// Package diagnostic defines closed, input-free diagnostics shared by sources
// and the configuration engine. It is not a public extension point.
package diagnostic

// Decode is a fixed document decoding failure, never a codec's raw error.
type Decode uint8

const (
	CodecMissing Decode = iota + 1
	EmptyDocument
	InvalidDocument
	NonObject
)

func (d Decode) Error() string {
	switch d {
	case CodecMissing:
		return "codec not registered"
	case EmptyDocument:
		return "document is empty"
	case InvalidDocument:
		return "document could not be decoded"
	case NonObject:
		return "document root must be an object"
	default:
		return "document could not be decoded"
	}
}
