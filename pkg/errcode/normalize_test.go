package errcode

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sync"
	"testing"
)

func TestNormalize_BuiltinClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want *AppError
	}{
		{"nil", nil, nil},
		{"empty join", errors.Join(nil, nil), nil},
		{"business error", ErrNotFound, ErrNotFound},
		{"wrapped business error", fmt.Errorf("load: %w", ErrPermissionDenied), ErrPermissionDenied},
		{"context deadline", fmt.Errorf("upstream: %w", context.DeadlineExceeded), ErrTimeout},
		{"os deadline", fmt.Errorf("read: %w", os.ErrDeadlineExceeded), ErrTimeout},
		{"network timeout", &net.DNSError{IsTimeout: true}, ErrTimeout},
		{"url timeout", &url.Error{Op: "Get", URL: "http://example.invalid", Err: os.ErrDeadlineExceeded}, ErrTimeout},
		{"network non-timeout", &net.DNSError{}, ErrInternal},
		{"cancellation", fmt.Errorf("request: %w", context.Canceled), ErrCanceled},
		{"unknown error", errors.New("secret path /etc/shadow"), ErrInternal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Normalize(tt.err)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("Normalize(%v) = %v; want nil *AppError", tt.err, got)
				}
				return
			}
			if got == nil || got.Code() != tt.want.Code() || got.Reason() != tt.want.Reason() || got.Message() != tt.want.Message() {
				t.Fatalf("Normalize(%v) = %v; want public fields of %v", tt.err, got, tt.want)
			}
			if !errors.Is(got, tt.err) {
				t.Fatal("classification lost the original error")
			}
		})
	}
}

func TestNormalize_IdentityAndIdempotence(t *testing.T) {
	if got := Normalize(ErrNotFound); got != ErrNotFound {
		t.Fatal("a direct *AppError must retain its identity")
	}
	for _, input := range []error{errors.New("raw"), ErrNotFound, fmt.Errorf("load: %w", ErrNotFound)} {
		got := Normalize(input)
		if got == nil || Normalize(got) != got {
			t.Fatal("normalizing a non-nil result must return that same *AppError")
		}
	}
}

type normalizationWrapper struct{ cause error }

func (e *normalizationWrapper) Error() string { return "load user: " + e.cause.Error() }
func (e *normalizationWrapper) Unwrap() error { return e.cause }

func TestNormalize_PreservesOuterWrapperAndJoinedBranches(t *testing.T) {
	raw := errors.New("storage failure")
	sibling := errors.New("audit failure")
	known := ErrNotFound.WithMessage("User not found.").KV("field", "user_id").Wrap(raw)
	outer := &normalizationWrapper{cause: known}
	for _, input := range []error{outer, errors.Join(nil, outer, sibling)} {
		got := Normalize(input)
		if got == known || got.Code() != known.Code() || got.Reason() != known.Reason() ||
			got.Message() != known.Message() || got.Metadata()["field"] != "user_id" {
			t.Fatalf("existing business fields were not copied: %v", got)
		}
		if errors.Unwrap(got) != input || !errors.Is(got, input) || !errors.Is(got, raw) {
			t.Fatal("the complete input must remain on the cause chain")
		}
		var recovered *normalizationWrapper
		if !errors.As(got, &recovered) || recovered != outer {
			t.Fatal("errors.As cannot reach the original outer wrapper")
		}
		if errors.Is(input, sibling) && !errors.Is(got, sibling) {
			t.Fatal("normalization discarded a joined sibling")
		}
	}
	if known.Unwrap() != raw {
		t.Fatal("normalization mutated the existing business error")
	}
}

func TestNormalize_JoinedClassificationOrder(t *testing.T) {
	joined := errors.Join(ErrNotFound, ErrConflict)
	got := Normalize(joined)
	if got.Reason() != ErrNotFound.Reason() || !errors.Is(got, ErrConflict) {
		t.Fatal("the first business error supplies fields; other branches remain reachable")
	}
	joined = errors.Join(context.Canceled, context.DeadlineExceeded)
	if got := Normalize(joined); got.Reason() != ErrTimeout.Reason() || !errors.Is(got, context.Canceled) {
		t.Fatal("timeout must precede cancellation while preserving both causes")
	}
}

func TestNormalize_TypedNilRemainsFailure(t *testing.T) {
	var typedNil *AppError
	sibling := errors.New("cleanup failure")
	inputs := []error{typedNil, fmt.Errorf("load: %w", typedNil), errors.Join(typedNil, sibling)}
	for _, input := range inputs {
		got := Normalize(input)
		if got == nil || got.Reason() != ErrInternal.Reason() {
			t.Fatalf("non-nil input %T must remain an internal error: %v", input, got)
		}
		if errors.Unwrap(got) != input || !errors.Is(got, input) {
			t.Fatal("typed-nil failure lost its original cause")
		}
		// 遍历到 nil 的 *AppError 接收者时也不能 panic。
		if errors.Is(got, ErrNotFound) {
			t.Fatal("typed-nil failure unexpectedly matched a business error")
		}
		if errors.Is(input, sibling) && !errors.Is(got, sibling) {
			t.Fatal("typed-nil failure discarded a joined sibling")
		}
		_ = got.Error()
		if status, body := Respond(got); status != 500 || body.Reason != ErrInternal.Reason() {
			t.Fatalf("typed-nil failure produced an incorrect response: %d %+v", status, body)
		}
	}
	if errors.Unwrap(typedNil) != nil {
		t.Fatal("a nil *AppError must not unwrap to another error")
	}
}

func TestNormalize_MappersAreOrderedAndScopedToCall(t *testing.T) {
	raw := errors.New("record missing")
	input := fmt.Errorf("query: %w", raw)
	calls := 0
	miss := func(err error) *AppError {
		calls++
		if err != input {
			t.Error("mapper did not receive the complete input")
		}
		return nil
	}
	match := func(err error) *AppError {
		calls++
		if errors.Is(err, raw) {
			return ErrNotFound.KV("field", "id")
		}
		return nil
	}
	unused := func(error) *AppError {
		t.Error("mapper after the first match must not run")
		return ErrConflict
	}
	got := Normalize(input, nil, miss, match, unused)
	if calls != 2 || got.Reason() != ErrNotFound.Reason() || got.Metadata()["field"] != "id" || errors.Unwrap(got) != input {
		t.Fatalf("mapper selection or cause preservation failed: calls=%d error=%v", calls, got)
	}
	if ErrNotFound.Unwrap() != nil || ErrNotFound.Metadata() != nil {
		t.Fatal("mapping mutated a shared predefined error")
	}
	if got := Normalize(input); got.Reason() != ErrInternal.Reason() {
		t.Fatal("one call's mapper changed another call's policy")
	}
}

func TestNormalize_ExplicitErrorThenMapperThenBuiltins(t *testing.T) {
	calls := 0
	mapper := func(error) *AppError {
		calls++
		return ErrConflict
	}
	if Normalize(nil, mapper) != nil || calls != 0 {
		t.Fatal("nil input must not invoke mappers")
	}
	known := ErrNotFound.Wrap(context.DeadlineExceeded)
	for _, input := range []error{known, fmt.Errorf("load: %w", known)} {
		if got := Normalize(input, mapper); got.Reason() != known.Reason() || calls != 0 {
			t.Fatal("an explicit business error must take precedence without invoking mappers")
		}
	}
	for _, input := range []error{context.DeadlineExceeded, context.Canceled} {
		got := Normalize(input, mapper)
		if got.Reason() != ErrConflict.Reason() || errors.Unwrap(got) != input {
			t.Fatalf("caller mapper must precede built-in classification: %v", got)
		}
	}
	if calls != 2 {
		t.Fatalf("mapper called %d times; want 2", calls)
	}
}

type normalizationNonTimeout struct{ error }

func (normalizationNonTimeout) Timeout() bool   { return false }
func (e normalizationNonTimeout) Unwrap() error { return e.error }

func TestNormalize_DeadlineIsNotHiddenByNonTimeoutWrapper(t *testing.T) {
	for _, deadline := range []error{context.DeadlineExceeded, os.ErrDeadlineExceeded} {
		input := fmt.Errorf("upstream: %w", normalizationNonTimeout{deadline})
		got := Normalize(input)
		if got.Reason() != ErrTimeout.Reason() || !errors.Is(got, deadline) || errors.Unwrap(got) != input {
			t.Fatalf("outer Timeout() == false hid the deadline or lost its chain: %v", got)
		}
	}
}

func TestNormalize_ConcurrentIndependentPolicies(t *testing.T) {
	raw := errors.New("backend failure")
	policies := []struct {
		mapper Mapper
		want   *AppError
	}{
		{func(error) *AppError { return ErrNotFound }, ErrNotFound},
		{func(error) *AppError { return ErrConflict }, ErrConflict},
		{nil, ErrInternal},
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		for _, policy := range policies {
			wg.Add(1)
			go func(mapper Mapper, want *AppError) {
				defer wg.Done()
				got := Normalize(raw, mapper)
				if got.Reason() != want.Reason() || got.Code() != want.Code() || errors.Unwrap(got) != raw {
					t.Errorf("independent mapper policy was changed: got %v, want %v", got, want)
				}
			}(policy.mapper, policy.want)
		}
	}
	wg.Wait()
}
