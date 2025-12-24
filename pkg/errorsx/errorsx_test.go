package errorsx

import (
	"fmt"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
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
