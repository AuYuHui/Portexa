package mt4

import (
	"context"
	"errors"
	"io"
	"net"

	"portexa/mt4/internal/protocol"
)

// ErrorCode is stable across operating systems and does not contain credentials.
type ErrorCode string

const (
	InvalidConfig       ErrorCode = "invalid_config"
	ReadOnlyRequired    ErrorCode = "read_only_required"
	UnsupportedBuild    ErrorCode = "unsupported_build"
	UnsupportedProtocol ErrorCode = "unsupported_protocol"
	Timeout             ErrorCode = "timeout"
	Canceled            ErrorCode = "canceled"
	ConnectionFailed    ErrorCode = "connection"
	ServerRejected      ErrorCode = "server_rejected"
	InvalidResponse     ErrorCode = "invalid_response"
	ResourceLimit       ErrorCode = "resource_limit"
)

// Error is a redacted failure. Unwrap preserves context and network error checks;
// applications should log Code/Operation rather than the underlying net.OpError.
// Status is the raw server code, not an inferred authentication/permission reason.
type Error struct {
	Code        ErrorCode
	Operation   string
	ServerBuild uint16
	Status      byte
	cause       error
}

func (e *Error) Error() string { return "mt4 " + e.Operation + ": " + string(e.Code) }
func (e *Error) Unwrap() error { return e.cause }

func classifyError(operation string, build uint16, err error) error {
	if err == nil {
		return nil
	}
	var phase *protocol.OperationError
	if errors.As(err, &phase) {
		operation = phase.Operation
	}
	failure := &Error{Code: InvalidResponse, Operation: operation, ServerBuild: build, cause: err}
	var status *protocol.StatusError
	var network net.Error
	var socket *net.OpError
	switch {
	case errors.Is(err, context.Canceled):
		failure.Code = Canceled
	case errors.Is(err, context.DeadlineExceeded):
		failure.Code = Timeout
	case errors.As(err, &network) && network.Timeout():
		failure.Code = Timeout
	case errors.Is(err, protocol.ErrUnsupportedBuild):
		failure.Code = UnsupportedBuild
	case errors.Is(err, protocol.ErrUnsupportedProtocol):
		failure.Code = UnsupportedProtocol
	case errors.Is(err, protocol.ErrReadOnlyCommand):
		failure.Code = ReadOnlyRequired
	case errors.Is(err, protocol.ErrResourceLimit):
		failure.Code = ResourceLimit
	case errors.As(err, &status):
		failure.Code, failure.Status = ServerRejected, status.Status
	case errors.As(err, &socket), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), errors.Is(err, net.ErrClosed):
		failure.Code = ConnectionFailed
	}
	return failure
}
