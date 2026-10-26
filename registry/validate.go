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
	ErrClosed = errors.New("registry: closed")
	// ErrNilInstance is returned when a nil *ServiceInstance is passed.
	ErrNilInstance = errors.New("registry: nil service instance")
	// ErrInvalidName is returned for an empty service name or one containing '/'.
	ErrInvalidName = errors.New("registry: invalid service name")
	// ErrInvalidID is returned for an instance ID containing '/', or an empty
	// ID where one is required (Deregister).
	ErrInvalidID = errors.New("registry: invalid instance id")
)

// ValidateName checks a service name. Names must be non-empty and must not
// contain '/', because backends use it as a path segment: "a" must never
// match the instances of "a/b".
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
func ValidateID(id string) error {
	if strings.Contains(id, "/") {
		return fmt.Errorf("%w: %q must not contain '/'", ErrInvalidID, id)
	}
	return nil
}

// Validate checks the fields every backend needs: a non-nil instance with a
// valid Name and ID.
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
func NewID() string {
	id := uuid.NewV4()
	return hex.EncodeToString(id[:])
}
