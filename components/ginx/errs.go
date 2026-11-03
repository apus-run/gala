package ginx

import "errors"

// ErrUnauthorized means the caller is not authenticated and the request must
// be rejected with 401 | ErrUnauthorized 表示未认证，请求需以 401 拒绝。
var ErrUnauthorized = errors.New("未授权")

// ErrNoResponse is a sentinel error.
// Returning ErrNoResponse tells ginx to skip writing the response, which
// usually means the handler has already written it back itself.
// ErrNoResponse 是一个 sentinel 错误：
// 返回它即告诉 ginx 不需要继续写响应，大多数情况下这意味着你已经写回了响应。
var ErrNoResponse = errors.New("不需要返回 response")
