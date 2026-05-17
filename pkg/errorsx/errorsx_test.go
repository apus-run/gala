package errorsx

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNew(t *testing.T) {
	e := A()

	fmt.Printf("%+v", e)
	fmt.Println()
	fmt.Println(e.Error())
}

func A() error {
	return B()
}

func B() error {
	return C()
}

func C() error {
	return InternalServer("InternalServer").
		WithCause(fmt.Errorf("db connection error")).
		WithStack()
}

func TestError(t *testing.T) {
	e := New(400, "InvalidParams").
		WithMessage("Invalid username format").
		WithDetails(map[string]string{
			"field":  "username",
			"format": "letters followed by 6 digits",
		})

	if e.Code != 400 || e.Status != "InvalidParams" || e.Message != "Invalid username format" {
		t.Errorf("unexpected error: %+v", e)
	}

	if e.Details.(map[string]string)["field"] != "username" || e.Details.(map[string]string)["format"] != "letters followed by 6 digits" {
		t.Errorf("unexpected metadata: %+v", e.Details)
	}
}

func TestWithMessage(t *testing.T) {
	e := New(400, "InvalidParams").
		WithMessage("Invalid username format").
		WithDetails(map[string]string{
			"field":  "username",
			"format": "letters followed by 6 digits",
		})

	if e.Code != 400 || e.Status != "InvalidParams" || e.Message != "Invalid username format" {
		t.Errorf("unexpected error: %+v", e)
	}

	if e.Details.(map[string]string)["field"] != "username" || e.Details.(map[string]string)["format"] != "letters followed by 6 digits" {
		t.Errorf("unexpected metadata: %+v", e.Details)
	}
}

func TestWithDetails(t *testing.T) {
	e := New(400, "InvalidParams").
		WithMessage("Invalid username format").
		WithDetails(map[string]string{
			"field":  "username",
			"format": "letters followed by 6 digits",
		})

	if e.Code != 400 || e.Status != "InvalidParams" || e.Message != "Invalid username format" {
		t.Errorf("unexpected error: %+v", e)
	}

	if e.Details.(map[string]string)["field"] != "username" || e.Details.(map[string]string)["format"] != "letters followed by 6 digits" {
		t.Errorf("unexpected metadata: %+v", e.Details)
	}
}

func TestWithCause(t *testing.T) {
	originalErr := fmt.Errorf("db connection error")
	e := New(500, "DBError").
		WithCause(originalErr).
		WithStack()

	if e.cause != originalErr {
		t.Errorf("unexpected cause: %+v", e.cause)
	}

	if len(e.stack) == 0 {
		t.Error("stack trace should not be empty")
	}
}

func TestKV(t *testing.T) {
	e := New(400, "ParseError").
		WithMessage("JSON parsing failed").
		KV("input", "{invalid: json}").
		KV("service", "user-api")

	if e.Details.(map[string]string)["input"] != "{invalid: json}" || e.Details.(map[string]string)["service"] != "user-api" {
		t.Errorf("unexpected metadata: %+v", e.Details)
	}
}

func TestKVOddPairsDoesNothing(t *testing.T) {
	e := New(400, "ParseError").KV("input")

	if e.Details != nil {
		t.Fatalf("expected odd key-value pairs to leave details unchanged, got %+v", e.Details)
	}
}

func TestGRPCStatus(t *testing.T) {
	e := New(500, "InternalError").
		WithMessage("Something went wrong")

	st := e.GRPCStatus()
	if st.Message() != "InternalError: Something went wrong" {
		t.Errorf("unexpected gRPC status message: %s", st.Message())
	}

	// 验证 ErrorInfo 详情
	details := st.Details()
	if len(details) != 0 {
		t.Errorf("unexpected gRPC status details: %+v", details)
	}

	// 测试带元数据的错误
	e2 := New(404, "USER_NOT_FOUND").
		WithMessage("User not found").
		WithDetails(map[string]string{
			"user_id": "12345",
			"domain":  "user-service",
		})

	st2 := e2.GRPCStatus()
	details2 := st2.Details()
	if len(details2) != 1 {
		t.Errorf("expected 1 detail, got %d", len(details2))
	}

	if info, ok := details2[0].(*errdetails.ErrorInfo); ok {
		if info.Reason != "USER_NOT_FOUND" {
			t.Errorf("unexpected reason: %s", info.Reason)
		}
		if info.Metadata["user_id"] != "12345" {
			t.Errorf("unexpected metadata: %+v", info.Metadata)
		}
	}
}

func TestComplexDetails(t *testing.T) {
	// 创建一个带复杂详情的错误（符合 AIP-193 标准）
	err := New(429, "RESOURCE_AVAILABILITY").
		WithMessage("The zone 'us-east1-a' does not have enough resources available to fulfill the request. Try a different zone, or try again later.").
		WithDetails(map[string]string{
			"zone":              "us-east1-a",
			"vmType":            "e2-medium",
			"attachment":        "local-ssd=3,nvidia-t4=2",
			"zonesWithCapacity": "us-central1-f,us-central1-c",
		})

	// 验证基本信息
	if err.Code != 429 {
		t.Errorf("expected code 429, got %d", err.Code)
	}
	if err.Status != "RESOURCE_AVAILABILITY" {
		t.Errorf("expected status RESOURCE_AVAILABILITY, got %s", err.Status)
	}

	// 验证详情
	details, ok := err.Details.(map[string]string)
	if !ok {
		t.Errorf("expected details to be map[string]string")
	}
	if details["zone"] != "us-east1-a" {
		t.Errorf("expected zone us-east1-a, got %s", details["zone"])
	}

	// 验证 gRPC 状态
	st := err.GRPCStatus()
	grpcDetails := st.Details()
	if len(grpcDetails) != 1 {
		t.Errorf("expected 1 gRPC detail, got %d", len(grpcDetails))
	}

	// 验证 ErrorInfo
	if info, ok := grpcDetails[0].(*errdetails.ErrorInfo); ok {
		if info.Reason != "RESOURCE_AVAILABILITY" {
			t.Errorf("expected reason RESOURCE_AVAILABILITY, got %s", info.Reason)
		}
		if info.Metadata["zone"] != "us-east1-a" {
			t.Errorf("expected metadata zone us-east1-a, got %s", info.Metadata["zone"])
		}
	}
}

func TestFromError(t *testing.T) {
	originalErr := fmt.Errorf("standard error")
	e := FromError(originalErr)

	if e.Code != 500 || e.Status != "InternalError" || e.Message != "standard error" {
		t.Errorf("unexpected error: %+v", e)
	}

	if e.cause != originalErr {
		t.Errorf("unexpected cause: %+v", e.cause)
	}
}

func TestFromErrorWithCustomError(t *testing.T) {
	original := NotFound(StatusNotFound).WithMessage("user not found")
	wrapped := fmt.Errorf("wrap: %w", original)

	got := FromError(wrapped)
	if got != original {
		t.Fatalf("expected FromError to return original *Error, got %+v", got)
	}
}

func TestFromErrorWithGRPCError(t *testing.T) {
	st := status.New(codes.NotFound, "USER_NOT_FOUND: user not found")
	st, err := st.WithDetails(&errdetails.ErrorInfo{
		Reason: "USER_NOT_FOUND",
		Metadata: map[string]string{
			"user_id": "12345",
		},
	})
	if err != nil {
		t.Fatalf("failed to attach grpc details: %v", err)
	}

	got := FromError(st.Err())
	if got.Code != http.StatusNotFound {
		t.Fatalf("expected HTTP 404, got %d", got.Code)
	}
	if got.Status != "USER_NOT_FOUND" {
		t.Fatalf("expected USER_NOT_FOUND, got %s", got.Status)
	}
	if got.Details.(map[string]string)["user_id"] != "12345" {
		t.Fatalf("unexpected details: %+v", got.Details)
	}
}

func TestCodeAndStatus(t *testing.T) {
	if Code(nil) != http.StatusOK {
		t.Fatalf("expected nil error code to be 200, got %d", Code(nil))
	}
	if Status(nil) != "" {
		t.Fatalf("expected nil error status to be empty, got %q", Status(nil))
	}

	err := Forbidden(StatusForbidden)
	if Code(err) != http.StatusForbidden {
		t.Fatalf("expected forbidden code, got %d", Code(err))
	}
	if Status(err) != StatusForbidden {
		t.Fatalf("expected forbidden status, got %s", Status(err))
	}
}

func TestErrorsIsAndUnwrap(t *testing.T) {
	cause := errors.New("db failed")
	err := DBReadError(StatusDBReadError).WithCause(cause)

	if !errors.Is(err, cause) {
		t.Fatal("expected errors.Is to match wrapped cause")
	}
	if !errors.Is(err, ErrDBReadError) {
		t.Fatal("expected errors.Is to match error template by code and status")
	}
	if errors.Is(err, ErrDBWriteError) {
		t.Fatal("did not expect DB read error to match DB write template")
	}
}

func TestClone(t *testing.T) {
	originalErr := fmt.Errorf("db connection error")
	e := New(500, "DB_ERROR").
		WithMessage("database failed").
		WithDetails(map[string]string{"operation": "read"}).
		WithCause(originalErr)

	cloned := e.Clone()
	if cloned == e {
		t.Fatal("clone should return a different Error instance")
	}
	if cloned.cause != originalErr {
		t.Fatalf("clone should preserve cause")
	}

	cloned.WithMessage("different message").KV("operation", "write")
	if e.Message != "database failed" {
		t.Fatalf("clone message mutation should not change original, got %q", e.Message)
	}
	if e.Details.(map[string]string)["operation"] != "read" {
		t.Fatalf("clone details mutation should not change original, got %+v", e.Details)
	}

	if len(e.stack) > 0 && len(cloned.stack) > 0 {
		cloned.stack[0] = "changed"
		if e.stack[0] == "changed" {
			t.Fatal("clone stack mutation should not change original")
		}
	}

	if (*Error)(nil).Clone() != nil {
		t.Fatal("nil clone should return nil")
	}
}

func TestCloneCopiesSupportedDetailsTypes(t *testing.T) {
	t.Run("map string string", func(t *testing.T) {
		e := New(400, "INVALID").WithDetails(map[string]string{"field": "name"})
		cloned := e.Clone()

		cloned.Details.(map[string]string)["field"] = "email"
		if e.Details.(map[string]string)["field"] != "name" {
			t.Fatalf("expected original details to be unchanged, got %+v", e.Details)
		}
	})

	t.Run("map string any", func(t *testing.T) {
		e := New(400, "INVALID").WithDetails(map[string]any{"field": "name"})
		cloned := e.Clone()

		cloned.Details.(map[string]any)["field"] = "email"
		if e.Details.(map[string]any)["field"] != "name" {
			t.Fatalf("expected original details to be unchanged, got %+v", e.Details)
		}
	})

	t.Run("string slice", func(t *testing.T) {
		e := New(400, "INVALID").WithDetails([]string{"name"})
		cloned := e.Clone()

		cloned.Details.([]string)[0] = "email"
		if e.Details.([]string)[0] != "name" {
			t.Fatalf("expected original details to be unchanged, got %+v", e.Details)
		}
	})
}

func TestCloneLeavesUnsupportedDetailsShared(t *testing.T) {
	type detail struct {
		Field string
	}

	d := &detail{Field: "name"}
	e := New(400, "INVALID").WithDetails(d)
	cloned := e.Clone()

	if cloned.Details != d {
		t.Fatalf("expected unsupported details to be copied by assignment")
	}
}

func TestPackageErrorTemplatesCloneBeforeMutation(t *testing.T) {
	cloned := ErrNotFound.Clone().
		WithMessage("user not found").
		KV("user_id", "12345")

	if cloned == ErrNotFound {
		t.Fatal("expected Clone to return a new instance")
	}
	if ErrNotFound.Message != "Not Found" {
		t.Fatalf("expected template message to remain unchanged, got %q", ErrNotFound.Message)
	}
	if ErrNotFound.Details != nil {
		t.Fatalf("expected template details to remain nil, got %+v", ErrNotFound.Details)
	}
}

func TestStatusConstantsBackTemplates(t *testing.T) {
	tests := []struct {
		name   string
		err    *Error
		status string
	}{
		{name: "ok", err: OK, status: StatusOK},
		{name: "not found", err: ErrNotFound, status: StatusNotFound},
		{name: "invalid params", err: ErrInvalidParams, status: StatusInvalidParams},
		{name: "db read", err: ErrDBReadError, status: StatusDBReadError},
		{name: "operation failed", err: ErrOperationFailed, status: StatusOperationFailed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Status != tt.status {
				t.Fatalf("expected status %s, got %s", tt.status, tt.err.Status)
			}
		})
	}
}

func TestEnableStackCapture(t *testing.T) {
	// Save the original value of EnableStackCapture to restore it later
	originalValue := EnableStackCapture
	defer func() { EnableStackCapture = originalValue }()

	// Test when EnableStackCapture is true
	EnableStackCapture = true
	e := New(500, "TestError").WithStack()
	if len(e.stack) == 0 {
		t.Error("stack trace should not be empty when EnableStackCapture is true")
	}

	// Test when EnableStackCapture is false
	EnableStackCapture = false
	e = New(500, "TestError").WithStack()
	if len(e.stack) != 0 {
		t.Error("stack trace should be empty when EnableStackCapture is false")
	}
}
