package protocol

import (
	"errors"
	"fmt"
)

var (
	ErrUnsupportedBuild    = errors.New("unsupported server build")
	ErrUnsupportedProtocol = errors.New("unsupported protocol profile")
	ErrResourceLimit       = errors.New("synchronization resource limit exceeded")
	ErrReadOnlyCommand     = errors.New("command is outside the read-only profile")
)

// StatusError retains a rejection code without guessing its undocumented meaning.
type StatusError struct {
	Status byte
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("server rejected request (status %d)", e.Status)
}

// OperationError identifies the failing phase, including failures after partial sync.
type OperationError struct {
	Operation string
	Err       error
}

func (e *OperationError) Error() string { return e.Operation + ": " + e.Err.Error() }
func (e *OperationError) Unwrap() error { return e.Err }

func operationError(operation string, err error) error {
	return &OperationError{Operation: operation, Err: err}
}
