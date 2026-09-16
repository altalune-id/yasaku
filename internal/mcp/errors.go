package mcp

import (
	"errors"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// UnauthenticatedError reports that the request carried no usable bearer token.
type UnauthenticatedError struct {
	Reason string
}

func (e *UnauthenticatedError) Error() string {
	if e.Reason == "" {
		return "mcp: unauthenticated"
	}
	return "mcp: unauthenticated: " + e.Reason
}

// ToAppError maps UnauthenticatedError to the MCP001 envelope.
func (*UnauthenticatedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeMCPUnauthenticated,
		"A valid bearer token is required",
		codes.Unauthenticated,
		&apperrorv1.ErrorDetail{Code: apperror.CodeMCPUnauthenticated},
	)
}

// IsUnauthenticatedError reports whether err's tree contains an *UnauthenticatedError.
func IsUnauthenticatedError(err error) bool {
	_, ok := errors.AsType[*UnauthenticatedError](err)
	return ok
}

// UnknownUserError reports that the token's subject matches no yasaku user.
type UnknownUserError struct {
	Subject string
}

func (e *UnknownUserError) Error() string {
	if e.Subject == "" {
		return "mcp: token subject is not a yasaku user"
	}
	return "mcp: token subject " + e.Subject + " is not a yasaku user"
}

// ToAppError maps UnknownUserError to the MCP003 envelope.
func (*UnknownUserError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeMCPUnknownUser,
		"This token's subject is not a yasaku user",
		codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodeMCPUnknownUser},
	)
}

// IsUnknownUserError reports whether err's tree contains an *UnknownUserError.
func IsUnknownUserError(err error) bool {
	_, ok := errors.AsType[*UnknownUserError](err)
	return ok
}

// NotMemberError reports that the caller belongs to no organization reachable over MCP.
type NotMemberError struct{}

func (*NotMemberError) Error() string { return "mcp: caller belongs to no organization" }

// ToAppError maps NotMemberError to the MCP004 envelope.
func (*NotMemberError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeMCPNotMember,
		"You belong to no organization",
		codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodeMCPNotMember},
	)
}

// IsNotMemberError reports whether err's tree contains a *NotMemberError.
func IsNotMemberError(err error) bool {
	_, ok := errors.AsType[*NotMemberError](err)
	return ok
}
