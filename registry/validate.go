package registry

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"uuid"
)

var (
	// ErrClosed is returned by operations on a closed Registry or Discovery.
	// 对已关闭的 Registry 或 Discovery 操作时返回 ErrClosed.
	ErrClosed = errors.New("registry: closed")
	// ErrNilInstance is returned when a nil *ServiceInstance is passed.
	// 传入 nil *ServiceInstance 时返回 ErrNilInstance.
	ErrNilInstance = errors.New("registry: nil service instance")
	// ErrInvalidName is returned for an empty service name or one containing '/'.
	// 服务名为空或含 '/' 时返回 ErrInvalidName.
	ErrInvalidName = errors.New("registry: invalid service name")
	// ErrInvalidID is returned for an instance ID containing '/', or an empty
	// ID where one is required (Deregister).
	// 实例 ID 含 '/'、或要求非空（Deregister）却为空时返回 ErrInvalidID.
	ErrInvalidID = errors.New("registry: invalid instance id")
)

// ValidateName checks a service name. Names must be non-empty and must not
// contain '/', because backends use it as a path segment: "a" must never
// match the instances of "a/b".
// ValidateName 校验服务名：必须非空且不含 '/'，因为后端把它当路径段用，
// "a" 绝不能匹配到 "a/b" 的实例.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty", ErrInvalidName)
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("%w: %q must not contain '/'", ErrInvalidName, name)
	}
	return nil
}

// ValidateID checks an instance ID. An empty ID is allowed here (Register
// generates one); it must not contain '/'.
// ValidateID 校验实例 ID：这里允许为空（Register 会生成），但不得含 '/'.
func ValidateID(id string) error {
	if strings.Contains(id, "/") {
		return fmt.Errorf("%w: %q must not contain '/'", ErrInvalidID, id)
	}
	return nil
}

// Validate checks the fields every backend needs: a non-nil instance with a
// valid Name and ID.
// Validate 校验所有后端都需要的字段：实例非 nil 且 Name 与 ID 合法.
func (ins *ServiceInstance) Validate() error {
	if ins == nil {
		return ErrNilInstance
	}
	if err := ValidateName(ins.Name); err != nil {
		return err
	}
	return ValidateID(ins.ID)
}

// NewID returns a new random instance ID containing 32 lowercase hex characters.
// NewID 返回新的随机实例 ID，由 32 个小写十六进制字符组成.
func NewID() string {
	id := uuid.NewV4()
	return hex.EncodeToString(id[:])
}
