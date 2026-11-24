package errcode_test

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/apus-run/gala/pkg/errcode"
)

var ErrUserNotFound = errcode.New(http.StatusNotFound, "NotFound.User", "User not found.")

func ExampleNew() {
	err := fmt.Errorf("load profile: %w", ErrUserNotFound.KV("id", "7"))

	fmt.Println(errors.Is(err, ErrUserNotFound))
	fmt.Println(errors.Is(err, errcode.ErrNotFound)) // 同为 404，但 Reason 不同
	// Output:
	// true
	// false
}

func ExampleNormalize() {
	// Mapper 把第三方错误翻译成业务错误；未识别的错误返回 nil，交给下一条规则。
	mapUser := func(err error) *errcode.AppError {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return nil
	}

	raw := fmt.Errorf("query user 7: %w", sql.ErrNoRows)
	e := errcode.Normalize(raw, mapUser)

	fmt.Println(e.Reason(), errors.Is(e, sql.ErrNoRows))             // 原始错误保留在错误链上
	fmt.Println(errcode.Normalize(errors.New("disk full")).Reason()) // 未知错误归为内部错误
	fmt.Println(errcode.Normalize(nil) == nil)                       // 成功请求
	// Output:
	// NotFound.User true
	// InternalError
	// true
}

func ExampleRespond() {
	e := errcode.Normalize(errors.New("dial postgres://user:pass@10.0.0.1:5432"))
	status, body := errcode.Respond(e)

	fmt.Println(status, body.Reason)
	fmt.Println(body.Detail) // 内部细节不会进入响应
	// Output:
	// 500 InternalError
	// Internal server error.
}

func ExampleAppHandler() {
	// 让示例输出稳定：日志写到标准输出，并去掉时间。
	prev := slog.Default()
	defer slog.SetDefault(prev)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})))

	h := errcode.AppHandler(func(http.ResponseWriter, *http.Request) *errcode.AppError {
		return ErrUserNotFound.KV("id", "7")
	})
	res := httptest.NewRecorder()
	h.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/users/7", http.NoBody))

	fmt.Println(res.Code, res.Header().Get("Content-Type"))
	fmt.Print(res.Body.String())
	// Output:
	// level=WARN msg="request failed" method=GET path=/users/7 err.code=404 err.reason=NotFound.User err.message="User not found." err.metadata=map[id:7]
	// 404 application/problem+json
	// {"title":"Not Found","status":404,"detail":"User not found.","reason":"NotFound.User","metadata":{"id":"7"}}
}
