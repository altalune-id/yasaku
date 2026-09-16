package mcp

import (
	"errors"
	"fmt"
)

// CodeUnexpected is the error code reported when no mapper claims a failure.
const CodeUnexpected = "GEN900"

// ErrorPayload is the JSON body of a failed tool call, mirroring apperror.v1.ErrorDetail.
type ErrorPayload struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Meta      map[string]string `json:"meta,omitempty"`
	RequestID string            `json:"request_id,omitempty"`
	TraceID   string            `json:"trace_id,omitempty"`
}

// ForbiddenScopeError reports that the caller lacks the scope a tool requires.
type ForbiddenScopeError struct {
	Tool string
	Need Scope
}

// Error implements the error interface.
func (e *ForbiddenScopeError) Error() string {
	return fmt.Sprintf("tool %s requires scope %s", e.Tool, e.Need)
}

// IsForbiddenScopeError reports whether err is a *ForbiddenScopeError.
func IsForbiddenScopeError(err error) bool {
	var target *ForbiddenScopeError
	return errors.As(err, &target)
}
