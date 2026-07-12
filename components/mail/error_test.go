package mail

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

func TestDeliveryErrorUnwrap(t *testing.T) {
	sentinel := errors.New("sdk failure")
	err := error(&DeliveryError{Provider: "ses", Code: "Throttling", Err: sentinel})

	if !errors.Is(err, sentinel) {
		t.Fatal("errors.Is must reach the wrapped sdk error")
	}
	var deliveryErr *DeliveryError
	if !errors.As(err, &deliveryErr) || deliveryErr.Code != "Throttling" {
		t.Fatalf("errors.As failed: %v", err)
	}
}

func TestDeliveryErrorMessage(t *testing.T) {
	withCode := &DeliveryError{Provider: "resend", Code: "rate_limited", Err: errors.New("429")}
	if got := withCode.Error(); got != "mail: resend send failed (rate_limited): 429" {
		t.Fatalf("unexpected message: %q", got)
	}
	withoutCode := &DeliveryError{Provider: "ses", Err: errors.New("boom")}
	if got := withoutCode.Error(); got != "mail: ses send failed: boom" {
		t.Fatalf("unexpected message: %q", got)
	}
}

func TestIsTransient(t *testing.T) {
	transient := &DeliveryError{Provider: "p", IsTransient: true, Err: errors.New("x")}
	if !IsTransient(transient) {
		t.Fatal("want transient")
	}
	if !IsTransient(fmt.Errorf("wrapped: %w", transient)) {
		t.Fatal("want transient through wrapping")
	}
	if IsTransient(&DeliveryError{Provider: "p", Err: errors.New("x")}) {
		t.Fatal("want not transient")
	}
	if IsTransient(errors.New("plain")) {
		t.Fatal("plain error is not transient")
	}
	if IsTransient(nil) {
		t.Fatal("nil is not transient")
	}
}

func TestMayHaveBeenAccepted(t *testing.T) {
	unknown := &DeliveryError{Provider: "p", Err: errors.New("x")}
	if unknown.Outcome != SendOutcomeUnknown {
		t.Fatal("zero value outcome must be SendOutcomeUnknown")
	}
	if !MayHaveBeenAccepted(unknown) {
		t.Fatal("unknown outcome must count as possibly accepted")
	}
	notAccepted := &DeliveryError{Provider: "p", Outcome: SendOutcomeNotAccepted, Err: errors.New("x")}
	if MayHaveBeenAccepted(notAccepted) {
		t.Fatal("not-accepted outcome must not count as possibly accepted")
	}
	if MayHaveBeenAccepted(context.Canceled) {
		t.Fatal("pre-call context error never reached the provider")
	}
	if MayHaveBeenAccepted(fmt.Errorf("%w: bad", ErrInvalidMessage)) {
		t.Fatal("validation error never reached the provider")
	}
	if MayHaveBeenAccepted(nil) {
		t.Fatal("nil error was not a failure")
	}
}
