package category

import (
	"errors"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// NotFoundError reports that no category matches ID.
type NotFoundError struct{ ID string }

func (e *NotFoundError) Error() string {
	return "category: lookup: not found: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeCategoryNotFound,
		"Category not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeCategoryNotFound,
			Meta: map[string]string{"category_id": e.ID},
		},
	)
}

// IsNotFoundError reports whether err's tree contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// AlreadyExistsError reports that the project already has a category with this slug.
type AlreadyExistsError struct{ Slug string }

func (e *AlreadyExistsError) Error() string {
	return "category: save: slug already taken in this project: slug=" + e.Slug
}

// ToAppError converts the typed error into the wire envelope.
func (e *AlreadyExistsError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeCategoryAlreadyExists,
		"A category with this slug already exists",
		codes.AlreadyExists,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeCategoryAlreadyExists,
			Meta: map[string]string{"slug": e.Slug},
		},
	)
}

// IsAlreadyExistsError reports whether err's tree contains an *AlreadyExistsError.
func IsAlreadyExistsError(err error) bool {
	_, ok := errors.AsType[*AlreadyExistsError](err)
	return ok
}

// InvalidNameError reports that a name violates a creation invariant.
type InvalidNameError struct{ Reason string }

func (e *InvalidNameError) Error() string {
	return "category: name: invalid: " + e.Reason
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidNameError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeCategoryInvalidName,
		"Invalid category name: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeCategoryInvalidName,
			Meta: map[string]string{"reason": e.Reason},
		},
	)
}

// IsInvalidNameError reports whether err's tree contains an *InvalidNameError.
func IsInvalidNameError(err error) bool {
	_, ok := errors.AsType[*InvalidNameError](err)
	return ok
}

// InUseError reports that the category still has posts.
type InUseError struct{ ID string }

func (e *InUseError) Error() string {
	return "category: delete: still referenced by posts: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (e *InUseError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeCategoryInUse,
		"This category still has posts",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeCategoryInUse,
			Meta: map[string]string{"category_id": e.ID},
		},
	)
}

// IsInUseError reports whether err's tree contains an *InUseError.
func IsInUseError(err error) bool {
	_, ok := errors.AsType[*InUseError](err)
	return ok
}
