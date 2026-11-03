package form

import (
	"encoding/base64"
	"fmt"
	"math"
	"strings"
	"time"

	"google.golang.org/protobuf/reflect/protoreflect"
)

const (
	// Timestamp well-known message | Timestamp well-known 消息.
	timestampMessageFullname    protoreflect.FullName    = "google.protobuf.Timestamp"
	maxTimestampSeconds                                  = 253402300799
	minTimestampSeconds                                  = -6213559680013
	timestampSecondsFieldNumber protoreflect.FieldNumber = 1
	timestampNanosFieldNumber   protoreflect.FieldNumber = 2

	// Duration well-known message | Duration well-known 消息.
	durationMessageFullname    protoreflect.FullName    = "google.protobuf.Duration"
	secondsInNanos                                      = 999999999
	durationSecondsFieldNumber protoreflect.FieldNumber = 1
	durationNanosFieldNumber   protoreflect.FieldNumber = 2

	// BytesValue well-known message | BytesValue well-known 消息.
	bytesMessageFullname  protoreflect.FullName    = "google.protobuf.BytesValue"
	bytesValueFieldNumber protoreflect.FieldNumber = 1

	// Struct well-known message | Struct well-known 消息.
	structMessageFullname   protoreflect.FullName    = "google.protobuf.Struct"
	structFieldsFieldNumber protoreflect.FieldNumber = 1

	// FieldMask well-known message | FieldMask well-known 消息.
	fieldMaskFullName protoreflect.FullName = "google.protobuf.FieldMask"
)

func marshalTimestamp(m protoreflect.Message) (string, error) {
	fds := m.Descriptor().Fields()
	fdSeconds := fds.ByNumber(timestampSecondsFieldNumber)
	fdNanos := fds.ByNumber(timestampNanosFieldNumber)

	secsVal := m.Get(fdSeconds)
	nanosVal := m.Get(fdNanos)
	secs := secsVal.Int()
	nanos := nanosVal.Int()
	if secs < minTimestampSeconds || secs > maxTimestampSeconds {
		return "", fmt.Errorf("%s: seconds out of range %v", timestampMessageFullname, secs)
	}
	if nanos < 0 || nanos > secondsInNanos {
		return "", fmt.Errorf("%s: nanos out of range %v", timestampMessageFullname, nanos)
	}
	// Uses RFC 3339, where generated output will be Z-normalized and uses 0, 3,
	// 6 or 9 fractional digits.
	// 使用 RFC 3339：输出做 Z 规范化，小数位为 0、3、6 或 9 位。
	t := time.Unix(secs, nanos).UTC()
	x := t.Format("2006-01-02T15:04:05.000000000")
	x = strings.TrimSuffix(x, "000")
	x = strings.TrimSuffix(x, "000")
	x = strings.TrimSuffix(x, ".000")
	return x + "Z", nil
}

func marshalDuration(m protoreflect.Message) (string, error) {
	fds := m.Descriptor().Fields()
	fdSeconds := fds.ByNumber(durationSecondsFieldNumber)
	fdNanos := fds.ByNumber(durationNanosFieldNumber)

	secsVal := m.Get(fdSeconds)
	nanosVal := m.Get(fdNanos)
	secs := secsVal.Int()
	nanos := nanosVal.Int()
	d := time.Duration(secs) * time.Second
	overflow := d/time.Second != time.Duration(secs)
	d += time.Duration(nanos) * time.Nanosecond
	overflow = overflow || (secs < 0 && nanos < 0 && d > 0)
	overflow = overflow || (secs > 0 && nanos > 0 && d < 0)
	if overflow {
		switch {
		case secs < 0:
			return time.Duration(math.MinInt64).String(), nil
		case secs > 0:
			return time.Duration(math.MaxInt64).String(), nil
		}
	}
	return d.String(), nil
}

func marshalBytes(m protoreflect.Message) (string, error) {
	fds := m.Descriptor().Fields()
	fdBytes := fds.ByNumber(bytesValueFieldNumber)
	bytesVal := m.Get(fdBytes)
	val := bytesVal.Bytes()
	return base64.StdEncoding.EncodeToString(val), nil
}
