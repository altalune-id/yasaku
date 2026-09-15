package category

import (
	"errors"
	"strconv"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// NotFoundError reports that no category matched the lookup.
type NotFoundError struct {
	ID   string
	Name string
}

func (e *NotFoundError) Error() string {
	if e.ID != "" {
		return "category: lookup: not found: id=" + e.ID
	}
	return "category: lookup: not found: name=" + e.Name
}

// ToAppError converts the typed error into the wire envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTxCategoryNotFound,
		"Category not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryNotFound,
			Meta: map[string]string{"category_id": e.ID, "name": e.Name},
		},
	)
}

// IsNotFoundError reports whether err's tree contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
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
		apperror.CodeTxCategoryInvalidName,
		"Invalid category name: "+e.Reason,
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryInvalidName,
			Meta: map[string]string{"field": "name", "reason": e.Reason},
		},
	)
}

// IsInvalidNameError reports whether err's tree contains an *InvalidNameError.
func IsInvalidNameError(err error) bool {
	_, ok := errors.AsType[*InvalidNameError](err)
	return ok
}

// InvalidIconError reports an icon outside AllowedIcons.
type InvalidIconError struct{ Icon string }

func (e *InvalidIconError) Error() string {
	return "category: icon: not in the allow-list: " + e.Icon
}

// ToAppError converts the typed error into the wire envelope. NOTE: CTG002 doubles as the appearance-field code; there is no separate icon code.
func (e *InvalidIconError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTxCategoryInvalidName,
		"That icon is not available",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryInvalidName,
			Meta: map[string]string{"field": "icon", "icon": e.Icon},
		},
	)
}

// IsInvalidIconError reports whether err's tree contains an *InvalidIconError.
func IsInvalidIconError(err error) bool {
	_, ok := errors.AsType[*InvalidIconError](err)
	return ok
}

// InvalidColorError reports a colour that is neither blank, chart-1..chart-5, nor #rrggbb.
type InvalidColorError struct{ Color string }

func (e *InvalidColorError) Error() string {
	return "category: color: must be blank, chart-1..chart-5 or #rrggbb: " + e.Color
}

// ToAppError converts the typed error into the wire envelope. NOTE: CTG002 doubles as the appearance-field code; there is no separate colour code.
func (e *InvalidColorError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTxCategoryInvalidName,
		"That colour is not available",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryInvalidName,
			Meta: map[string]string{"field": "color", "color": e.Color},
		},
	)
}

// IsInvalidColorError reports whether err's tree contains an *InvalidColorError.
func IsInvalidColorError(err error) bool {
	_, ok := errors.AsType[*InvalidColorError](err)
	return ok
}

// AlreadyExistsError reports that the project already has an active category with this name under this kind.
type AlreadyExistsError struct {
	Name string
	Kind Kind
}

func (e *AlreadyExistsError) Error() string {
	return "category: save: name already used in this project: kind=" + string(e.Kind) + " name=" + e.Name
}

// ToAppError converts the typed error into the wire envelope.
func (e *AlreadyExistsError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTxCategoryAlreadyExists,
		"A category with this name already exists",
		codes.AlreadyExists,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryAlreadyExists,
			Meta: map[string]string{"name": e.Name, "kind": string(e.Kind)},
		},
	)
}

// IsAlreadyExistsError reports whether err's tree contains an *AlreadyExistsError.
func IsAlreadyExistsError(err error) bool {
	_, ok := errors.AsType[*AlreadyExistsError](err)
	return ok
}

// InvalidKindError reports a kind that is neither expense nor income.
type InvalidKindError struct{ Value string }

func (e *InvalidKindError) Error() string {
	return "category: kind: must be expense or income: " + e.Value
}

// ToAppError converts the typed error into the wire envelope.
func (e *InvalidKindError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTxCategoryInvalidKind,
		"A category must be either an expense or an income category",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryInvalidKind,
			Meta: map[string]string{"kind": e.Value},
		},
	)
}

// IsInvalidKindError reports whether err's tree contains an *InvalidKindError.
func IsInvalidKindError(err error) bool {
	_, ok := errors.AsType[*InvalidKindError](err)
	return ok
}

// InUseError reports that the category is still referenced by transactions.
type InUseError struct{ ID string }

func (e *InUseError) Error() string {
	return "category: delete: still referenced by transactions: id=" + e.ID
}

// ToAppError converts the typed error into the wire envelope.
func (e *InUseError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTxCategoryInUse,
		"This category is still used by transactions. Archive it instead.",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryInUse,
			Meta: map[string]string{"category_id": e.ID},
		},
	)
}

// IsInUseError reports whether err's tree contains an *InUseError.
func IsInUseError(err error) bool {
	_, ok := errors.AsType[*InUseError](err)
	return ok
}

// AmbiguousNameError reports that a name lookup matched more than one category.
type AmbiguousNameError struct {
	Name    string
	Matches int
}

func (e *AmbiguousNameError) Error() string {
	return "category: resolve: " + strconv.Itoa(e.Matches) + " categories match: name=" + e.Name
}

// ToAppError converts the typed error into the wire envelope.
func (e *AmbiguousNameError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeTxCategoryAmbiguousName,
		"That name matches more than one category",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeTxCategoryAmbiguousName,
			Meta: map[string]string{"name": e.Name, "matches": strconv.Itoa(e.Matches)},
		},
	)
}

// IsAmbiguousNameError reports whether err's tree contains an *AmbiguousNameError.
func IsAmbiguousNameError(err error) bool {
	_, ok := errors.AsType[*AmbiguousNameError](err)
	return ok
}
