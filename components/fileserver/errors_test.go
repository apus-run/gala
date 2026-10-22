package fileserver

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestErrorFormatting(t *testing.T) {
	withKey := &Error{Op: "Upload", Key: "a/b", Err: ErrNotFound}
	if got, want := withKey.Error(), `fileserver: Upload "a/b": fileserver: not found`; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	noKey := &Error{Op: "Begin", Err: ErrInvalidArgument}
	if got, want := noKey.Error(), "fileserver: Begin: fileserver: invalid argument"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
}

func TestErrorUnwrapsSentinelsAndCauses(t *testing.T) {
	cause := errors.New("sdk: connection reset")
	wrapped := &Error{
		Op:  "Upload",
		Key: "k",
		Err: fmt.Errorf("%w: %w", ErrIndeterminate, cause),
	}
	if !errors.Is(wrapped, ErrIndeterminate) {
		t.Fatal("errors.Is does not find the sentinel")
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("errors.Is does not find the original cause")
	}

	ctxErr := &Error{Op: "Read", Key: "k", Err: context.Canceled}
	if !errors.Is(ctxErr, context.Canceled) {
		t.Fatal("context error is not visible through *Error")
	}
}

func TestErrorAsRecoversOpAndKey(t *testing.T) {
	var err error = fmt.Errorf("outer: %w", &Error{Op: "Stat", Key: "x/y", Err: ErrNotFound})
	var fe *Error
	if !errors.As(err, &fe) {
		t.Fatal("errors.As did not find *Error")
	}
	if fe.Op != "Stat" || fe.Key != "x/y" {
		t.Fatalf("recovered Op=%q Key=%q", fe.Op, fe.Key)
	}
}

func TestRetryable(t *testing.T) {
	retryable := []error{
		ErrIntegrity,
		ErrThrottled,
		fmt.Errorf("Upload: %w: backend checksum mismatch", ErrIntegrity),
		&Error{Op: "Read", Key: "k", Err: fmt.Errorf("%w: response ended with 3 bytes still expected", io.ErrUnexpectedEOF)},
	}
	for _, err := range retryable {
		if !Retryable(err) {
			t.Errorf("Retryable(%v) = false, want true", err)
		}
	}
	notRetryable := []error{
		nil,
		ErrIndeterminate,
		fmt.Errorf("%w: %w", ErrIndeterminate, io.ErrUnexpectedEOF),
		errors.Join(ErrIntegrity, ErrIndeterminate),
		errors.Join(io.ErrUnexpectedEOF, ErrConflict),
		ErrConflict,
		ErrNotFound,
		ErrInvalidArgument,
		context.Canceled,
		errors.New("unknown"),
	}
	for _, err := range notRetryable {
		if Retryable(err) {
			t.Errorf("Retryable(%v) = true, want false", err)
		}
	}
}

func TestProgressFuncNilIsNoOp(t *testing.T) {
	var p ProgressFunc
	p.Report(context.Background(), ProgressInfo{Key: "k"})
}

func TestProgressFuncReceivesReports(t *testing.T) {
	var got ProgressInfo
	p := ProgressFunc(func(_ context.Context, info ProgressInfo) { got = info })
	p.Report(context.Background(), ProgressInfo{Key: "k", Transferred: 7, Total: -1})
	if got.Key != "k" || got.Transferred != 7 || got.Total != -1 {
		t.Fatalf("got %+v", got)
	}
}
