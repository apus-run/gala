package errorsx

import (
	"fmt"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// DemoComplexError 展示符合 AIP-193 标准的复杂错误用法
func DemoComplexError() {
	// 创建一个带详细信息的错误
	err := New(429, "RESOURCE_AVAILABILITY").
		WithMessage("The zone 'us-east1-a' does not have enough resources available to fulfill the request. Try a different zone, or try again later.").
		WithDetails(map[string]string{
			"zone":              "us-east1-a",
			"vmType":            "e2-medium",
			"attachment":        "local-ssd=3,nvidia-t4=2",
			"zonesWithCapacity": "us-central1-f,us-central1-c",
		})

	fmt.Printf("Error: %v\n", err)
	fmt.Printf("Code: %d, Status: %s, Message: %s\n", err.Code, err.Status, err.Message)
	fmt.Printf("Details: %+v\n", err.Details)

	// 获取 gRPC 状态（自动包含 ErrorInfo）
	st := err.GRPCStatus()
	fmt.Printf("gRPC Status: %s\n", st.Message())
	fmt.Printf("gRPC Details count: %d\n", len(st.Details()))

	// Output:
	// Error: code: 429, status: RESOURCE_AVAILABILITY, message: The zone 'us-east1-a' does not have enough resources available to fulfill the request. Try a different zone, or try again later., details: map[attachment:local-ssd=3,nvidia-t4=2 vmType:e2-medium zone:us-east1-a zonesWithCapacity:us-central1-f,us-central1-c]
	// Code: 429, Status: RESOURCE_AVAILABILITY, Message: The zone 'us-east1-a' does not have enough resources available to fulfill the request. Try a different zone, or try again later.
	// Details: map[attachment:local-ssd=3,nvidia-t4=2 vmType:e2-medium zone:us-east1-a zonesWithCapacity:us-central1-f,us-central1-c]
	// gRPC Status: RESOURCE_AVAILABILITY: The zone 'us-east1-a' does not have enough resources available to fulfill the request. Try a different zone, or try again later.
	// gRPC Details count: 1
}

// DemoMultipleDetails 展示如何手动创建包含多种详情类型的错误
func DemoMultipleDetails() {
	// 创建基础错误
	err := New(400, "INVALID_REQUEST").
		WithMessage("Invalid request parameters")

	// 获取 gRPC 状态
	st := err.GRPCStatus()

	// 手动添加多种详情类型（符合 AIP-193 标准）
	st, _ = st.WithDetails(&errdetails.ErrorInfo{
		Reason: "INVALID_REQUEST",
		Domain: "gala.apis",
		Metadata: map[string]string{
			"field":  "username",
			"reason": "does not match pattern",
		},
	})

	st, _ = st.WithDetails(&errdetails.LocalizedMessage{
		Locale:  "en-US",
		Message: "The username does not match the required pattern. Please use only letters followed by 6 digits.",
	})

	st, _ = st.WithDetails(&errdetails.Help{
		Links: []*errdetails.Help_Link{
			{
				Description: "Username validation rules",
				Url:         "https://docs.example.com/validation#username",
			},
		},
	})

	fmt.Printf("gRPC Status: %s\n", st.Message())
	fmt.Printf("Details count: %d\n", len(st.Details()))

	for i, detail := range st.Details() {
		fmt.Printf("Detail %d: %T\n", i, detail)
	}

	// Output:
	// gRPC Status: INVALID_REQUEST: Invalid request parameters
	// Details count: 3
	// Detail 0: *errdetails.ErrorInfo
	// Detail 1: *errdetails.LocalizedMessage
	// Detail 2: *errdetails.Help
}

// DemoSimpleError 展示简单错误的用法
func DemoSimpleError() {
	// 使用预定义的错误类型
	err := NotFound("USER_NOT_FOUND").
		WithMessage("User not found").
		WithDetails(map[string]string{
			"user_id": "12345",
		})

	if IsNotFound(err) {
		fmt.Printf("This is a NotFound error: %s\n", err.Message)
	}

	// Output:
	// This is a NotFound error: User not found
}
