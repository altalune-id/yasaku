package dataplane

import (
	"encoding/json"
	"errors"
	"net/http"

	"altalune.id/yasaku/internal/blog"
)

// NotFoundError is the single opaque "does not exist" outcome for an unresolvable scope, a missing resource or a denied authorization.
type NotFoundError struct{}

func (*NotFoundError) Error() string { return "dataplane: not found" }

// IsNotFoundError reports whether err is a *NotFoundError.
func IsNotFoundError(err error) bool {
	var target *NotFoundError
	return errors.As(err, &target)
}

// UnauthorizedError is the single opaque authentication failure.
type UnauthorizedError struct{}

func (*UnauthorizedError) Error() string { return "dataplane: unauthorized" }

// IsUnauthorizedError reports whether err is an *UnauthorizedError.
func IsUnauthorizedError(err error) bool {
	var target *UnauthorizedError
	return errors.As(err, &target)
}

// MethodNotAllowedError reports a request whose path is routed but whose method is not.
type MethodNotAllowedError struct{}

func (*MethodNotAllowedError) Error() string { return "dataplane: method not allowed" }

// IsMethodNotAllowedError reports whether err is a *MethodNotAllowedError.
func IsMethodNotAllowedError(err error) bool {
	var target *MethodNotAllowedError
	return errors.As(err, &target)
}

// PreconditionFailedError reports a conditional write whose If-Match no longer holds.
type PreconditionFailedError struct{}

func (*PreconditionFailedError) Error() string { return "dataplane: precondition failed" }

// IsPreconditionFailedError reports whether err is a *PreconditionFailedError.
func IsPreconditionFailedError(err error) bool {
	var target *PreconditionFailedError
	return errors.As(err, &target)
}

// PreconditionRequiredError reports a mutating request that carried no If-Match at all.
type PreconditionRequiredError struct{}

func (*PreconditionRequiredError) Error() string { return "dataplane: precondition required" }

// IsPreconditionRequiredError reports whether err is a *PreconditionRequiredError.
func IsPreconditionRequiredError(err error) bool {
	var target *PreconditionRequiredError
	return errors.As(err, &target)
}

// BadRequestError reports a malformed request body, header or identifier.
type BadRequestError struct{}

func (*BadRequestError) Error() string { return "dataplane: bad request" }

// IsBadRequestError reports whether err is a *BadRequestError.
func IsBadRequestError(err error) bool {
	var target *BadRequestError
	return errors.As(err, &target)
}

// ConflictError reports a slug already in use, or an Idempotency-Key replayed with a different body.
type ConflictError struct{}

func (*ConflictError) Error() string { return "dataplane: conflict" }

// IsConflictError reports whether err is a *ConflictError.
func IsConflictError(err error) bool {
	var target *ConflictError
	return errors.As(err, &target)
}

// InProgressError reports an Idempotency-Key whose first request has not answered yet.
type InProgressError struct{}

func (*InProgressError) Error() string { return "dataplane: request in progress" }

// IsInProgressError reports whether err is an *InProgressError.
func IsInProgressError(err error) bool {
	var target *InProgressError
	return errors.As(err, &target)
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SECURITY: an unrecognized error type falls through to 500, never admitted as success.
func writeError(w http.ResponseWriter, err error) {
	f := statusFor(err)
	w.Header().Del("ETag")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(f.status)
	_ = json.NewEncoder(w).Encode(errorBody{Code: f.code, Message: f.code})
}

type failure struct {
	status int
	code   string
}

//nolint:cyclop // a flat mapping table; splitting it hides the contract it states.
func statusFor(err error) failure {
	switch {
	case IsNotFoundError(err), blog.IsNotFoundError(err):
		return failure{http.StatusNotFound, "not_found"}
	case IsUnauthorizedError(err):
		return failure{http.StatusUnauthorized, "unauthorized"}
	case IsMethodNotAllowedError(err):
		return failure{http.StatusMethodNotAllowed, "method_not_allowed"}
	case IsPreconditionRequiredError(err):
		return failure{http.StatusPreconditionRequired, "precondition_required"}
	case IsPreconditionFailedError(err), blog.IsStaleVersionError(err):
		return failure{http.StatusPreconditionFailed, "precondition_failed"}
	case IsConflictError(err), blog.IsAlreadyExistsError(err):
		return failure{http.StatusConflict, "conflict"}
	case IsInProgressError(err):
		return failure{http.StatusConflict, "in_progress"}
	case IsBadRequestError(err), isInvalidPost(err):
		return failure{http.StatusBadRequest, "bad_request"}
	}
	return failure{http.StatusInternalServerError, "internal"}
}

func isInvalidPost(err error) bool {
	return blog.IsInvalidTitleError(err) ||
		blog.IsInvalidSlugError(err) ||
		blog.IsInvalidBodyError(err) ||
		blog.IsCategoryRequiredError(err)
}
