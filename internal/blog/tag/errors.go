package tag

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// NotFoundError reports that no tag matched the lookup.
type NotFoundError struct {
	ID string
}

func (e *NotFoundError) Error() string {
	if e.ID == "" {
		return "tag: not found"
	}
	return fmt.Sprintf("tag: not found: id=%s", e.ID)
}

// ToAppError converts the typed error into the wire envelope.
func (*NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTagNotFound,
		"Tag not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{Code: apperror.CodeTagNotFound},
	)
}

// IsNotFoundError reports whether err's tree contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// AlreadyExistsError reports that the project already has a tag with that slug.
type AlreadyExistsError struct {
	Slug string
}

func (e *AlreadyExistsError) Error() string {
	if e.Slug == "" {
		return "tag: already exists"
	}
	return fmt.Sprintf("tag: already exists: slug=%s", e.Slug)
}

// ToAppError converts the typed error into the wire envelope.
func (*AlreadyExistsError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTagAlreadyExists,
		"A tag with that slug already exists in this project",
		codes.AlreadyExists,
		&apperrorv1.ErrorDetail{Code: apperror.CodeTagAlreadyExists},
	)
}

// IsAlreadyExistsError reports whether err's tree contains an *AlreadyExistsError.
func IsAlreadyExistsError(err error) bool {
	_, ok := errors.AsType[*AlreadyExistsError](err)
	return ok
}

// InvalidNameError reports a name that fails the aggregate's invariants.
type InvalidNameError struct {
	Reason string
}

func (e *InvalidNameError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "invalid"
	}
	return "tag: invalid name: " + reason
}

// ToAppError converts the typed error into the wire envelope.
func (*InvalidNameError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTagInvalidName,
		"Invalid tag name",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodeTagInvalidName},
	)
}

// IsInvalidNameError reports whether err's tree contains an *InvalidNameError.
func IsInvalidNameError(err error) bool {
	_, ok := errors.AsType[*InvalidNameError](err)
	return ok
}

// InUseError reports that the tag is still attached to posts.
type InUseError struct {
	ID string
}

func (e *InUseError) Error() string {
	return "tag: delete: still referenced by posts: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (*InUseError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTagInUse,
		"This tag is still attached to posts",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{Code: apperror.CodeTagInUse},
	)
}

// IsInUseError reports whether err's tree contains an *InUseError.
func IsInUseError(err error) bool {
	_, ok := errors.AsType[*InUseError](err)
	return ok
}
