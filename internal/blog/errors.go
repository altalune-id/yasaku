package blog

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// NotFoundError reports that no post matched the lookup.
type NotFoundError struct {
	ID string
}

func (e *NotFoundError) Error() string {
	if e.ID == "" {
		return "blog: post not found"
	}
	return fmt.Sprintf("blog: post not found: id=%s", e.ID)
}

// ToAppError converts the typed error into the wire envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePostNotFound,
		"Post not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{Code: apperror.CodePostNotFound},
	)
}

// IsNotFoundError reports whether err's tree contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// AlreadyExistsError reports that the project already has a post with this slug.
type AlreadyExistsError struct {
	Slug string
}

func (e *AlreadyExistsError) Error() string {
	if e.Slug == "" {
		return "blog: post already exists"
	}
	return fmt.Sprintf("blog: post already exists: slug=%s", e.Slug)
}

// ToAppError converts the typed error into the wire envelope.
func (e *AlreadyExistsError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePostAlreadyExists,
		"A post with this slug already exists",
		codes.AlreadyExists,
		&apperrorv1.ErrorDetail{Code: apperror.CodePostAlreadyExists},
	)
}

// IsAlreadyExistsError reports whether err's tree contains an *AlreadyExistsError.
func IsAlreadyExistsError(err error) bool {
	_, ok := errors.AsType[*AlreadyExistsError](err)
	return ok
}

// InvalidTitleError reports that the post title breaks its invariants.
type InvalidTitleError struct {
	Reason string
}

func (e *InvalidTitleError) Error() string {
	return fmt.Sprintf("blog: invalid post title: %s", e.Reason)
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidTitleError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePostInvalidTitle,
		"Post title is invalid",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodePostInvalidTitle},
	)
}

// IsInvalidTitleError reports whether err's tree contains an *InvalidTitleError.
func IsInvalidTitleError(err error) bool {
	_, ok := errors.AsType[*InvalidTitleError](err)
	return ok
}

// InvalidSlugError reports that the post slug breaks its invariants.
type InvalidSlugError struct {
	Reason string
}

func (e *InvalidSlugError) Error() string {
	return fmt.Sprintf("blog: invalid post slug: %s", e.Reason)
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidSlugError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePostInvalidSlug,
		"Post slug is invalid",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodePostInvalidSlug},
	)
}

// IsInvalidSlugError reports whether err's tree contains an *InvalidSlugError.
func IsInvalidSlugError(err error) bool {
	_, ok := errors.AsType[*InvalidSlugError](err)
	return ok
}

// InvalidBodyError reports that the post body breaks its invariants.
type InvalidBodyError struct {
	Reason string
}

func (e *InvalidBodyError) Error() string {
	return fmt.Sprintf("blog: invalid post body: %s", e.Reason)
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidBodyError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePostInvalidBody,
		"Post body is invalid",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodePostInvalidBody},
	)
}

// IsInvalidBodyError reports whether err's tree contains an *InvalidBodyError.
func IsInvalidBodyError(err error) bool {
	_, ok := errors.AsType[*InvalidBodyError](err)
	return ok
}

// CategoryRequiredError reports that the post names no category.
type CategoryRequiredError struct{}

func (e *CategoryRequiredError) Error() string {
	return "blog: invalid post: category is required"
}

// ToAppError converts the typed error into the wire envelope.
func (e *CategoryRequiredError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodePostCategoryRequired,
		"A post needs a category",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodePostCategoryRequired},
	)
}

// IsCategoryRequiredError reports whether err's tree contains a *CategoryRequiredError.
func IsCategoryRequiredError(err error) bool {
	_, ok := errors.AsType[*CategoryRequiredError](err)
	return ok
}
