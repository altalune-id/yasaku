package user

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
)

// SingletonOrgMissingError signals the selfhosted singleton org has not been provisioned yet — the caller (typically OnboardWorkflow) should treat it as "onboarding still needs to run".
type SingletonOrgMissingError struct{}

func (e *SingletonOrgMissingError) Error() string {
	return "user: singleton org not provisioned"
}

// IsSingletonOrgMissingError reports whether err's tree contains a *SingletonOrgMissingError.
func IsSingletonOrgMissingError(err error) bool {
	_, ok := errors.AsType[*SingletonOrgMissingError](err)
	return ok
}

// NotFoundError signals no user matched the lookup.
type NotFoundError struct {
	ID      string
	Email   string
	Subject string
}

func (e *NotFoundError) Error() string {
	switch {
	case e.ID != "":
		return "user: not found: id=" + e.ID
	case e.Email != "":
		return "user: not found: email=" + e.Email
	case e.Subject != "":
		return "user: not found: idp_subject=" + e.Subject
	default:
		return "user: not found"
	}
}

// ToAppError maps NotFoundError to the canonical NotFound envelope.
func (e *NotFoundError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeUserNotFound,
		"User not found",
		codes.NotFound,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserNotFound},
	)
}

// IsNotFoundError reports whether err's tree contains a *NotFoundError.
func IsNotFoundError(err error) bool {
	_, ok := errors.AsType[*NotFoundError](err)
	return ok
}

// AlreadyExistsError signals a uniqueness violation on save.
type AlreadyExistsError struct {
	Field string
	Value string
}

func (e *AlreadyExistsError) Error() string {
	if e.Field == "" {
		return "user: already exists"
	}
	return fmt.Sprintf("user: already exists: %s=%q", e.Field, e.Value)
}

// ToAppError maps AlreadyExistsError to the canonical AlreadyExists envelope.
func (e *AlreadyExistsError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeUserAlreadyExists,
		"User already exists",
		codes.AlreadyExists,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserAlreadyExists},
	)
}

// IsAlreadyExistsError reports whether err's tree contains an *AlreadyExistsError.
func IsAlreadyExistsError(err error) bool {
	_, ok := errors.AsType[*AlreadyExistsError](err)
	return ok
}

// NotInvitedError signals a selfhosted OIDC login with no membership and no invite.
type NotInvitedError struct {
	Email string
}

func (e *NotInvitedError) Error() string {
	if e.Email == "" {
		return "user: not invited"
	}
	return "user: not invited: email=" + e.Email
}

// ToAppError maps NotInvitedError to the canonical PermissionDenied envelope.
func (e *NotInvitedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeUserNotInvited,
		"User has no membership and no pending invite",
		codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserNotInvited},
	)
}

// IsNotInvitedError reports whether err's tree contains a *NotInvitedError.
func IsNotInvitedError(err error) bool {
	_, ok := errors.AsType[*NotInvitedError](err)
	return ok
}

// InvalidEmailError signals a malformed or empty email.
type InvalidEmailError struct {
	Reason string
	Value  string
}

func (e *InvalidEmailError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "invalid"
	}
	if e.Value == "" {
		return "user: email: " + reason
	}
	return fmt.Sprintf("user: email: %s: %q", reason, e.Value)
}

// ToAppError maps InvalidEmailError to the canonical InvalidArgument envelope.
func (e *InvalidEmailError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeUserInvalidEmail,
		"Invalid user email",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserInvalidEmail},
	)
}

// IsInvalidEmailError reports whether err's tree contains an *InvalidEmailError.
func IsInvalidEmailError(err error) bool {
	_, ok := errors.AsType[*InvalidEmailError](err)
	return ok
}

// SignupRequiredError signals an OIDC login with no membership and no invite it could accept, signed in with no tenant; JoinRefused names an invite a limit refused.
type SignupRequiredError struct {
	UserID      string
	Email       string
	JoinRefused error
}

func (e *SignupRequiredError) Error() string {
	if e.Email == "" {
		return "user: signup required"
	}
	return "user: signup required: email=" + e.Email
}

// ToAppError maps SignupRequiredError to the canonical FailedPrecondition envelope.
func (e *SignupRequiredError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeSignupRequired,
		"Signup completion required",
		codes.FailedPrecondition,
		&apperrorv1.ErrorDetail{Code: apperror.CodeSignupRequired},
	)
}

// IsSignupRequiredError reports whether err's tree contains a *SignupRequiredError.
func IsSignupRequiredError(err error) bool {
	_, ok := errors.AsType[*SignupRequiredError](err)
	return ok
}

// InvalidNameError signals an empty or invalid display name.
type InvalidNameError struct {
	Reason string
}

func (e *InvalidNameError) Error() string {
	reason := e.Reason
	if reason == "" {
		reason = "invalid"
	}
	return "user: name: " + reason
}

// ToAppError maps InvalidNameError to the canonical InvalidArgument envelope.
func (e *InvalidNameError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeUserInvalidName,
		"Invalid user name",
		codes.InvalidArgument,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserInvalidName},
	)
}

// IsInvalidNameError reports whether err's tree contains an *InvalidNameError.
func IsInvalidNameError(err error) bool {
	_, ok := errors.AsType[*InvalidNameError](err)
	return ok
}

// EmailUnverifiedError signals an OIDC sign-in whose email the issuer has not verified, for a subject not yet linked.
type EmailUnverifiedError struct{}

func (*EmailUnverifiedError) Error() string {
	return "user: oidc email not verified"
}

// ToAppError maps EmailUnverifiedError to the canonical PermissionDenied envelope.
func (*EmailUnverifiedError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeUserEmailUnverified,
		"The identity provider has not verified this email",
		codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserEmailUnverified},
	)
}

// IsEmailUnverifiedError reports whether err's tree contains an *EmailUnverifiedError.
func IsEmailUnverifiedError(err error) bool {
	_, ok := errors.AsType[*EmailUnverifiedError](err)
	return ok
}

// IdentityConflictError signals an OIDC sign-in whose verified email belongs to another user; Cause is the store error, if any.
type IdentityConflictError struct{ Cause error }

func (*IdentityConflictError) Error() string {
	return "user: oidc email belongs to another user"
}

func (e *IdentityConflictError) Unwrap() error { return e.Cause }

// ToAppError maps IdentityConflictError to the canonical PermissionDenied envelope.
func (*IdentityConflictError) ToAppError() *apperror.AppError {
	return apperror.New(
		apperror.CodeUserIdentityConflict,
		"This email belongs to another account at the identity provider",
		codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserIdentityConflict},
	)
}

// IsIdentityConflictError reports whether err's tree contains an *IdentityConflictError.
func IsIdentityConflictError(err error) bool {
	_, ok := errors.AsType[*IdentityConflictError](err)
	return ok
}

// TipsSeenContendedError signals MarkTipsSeen lost every compare-and-swap round to concurrent writers.
type TipsSeenContendedError struct {
	ID       string
	Attempts int
}

func (e *TipsSeenContendedError) Error() string {
	return fmt.Sprintf("user: tips_seen kept changing: id=%s attempts=%d", e.ID, e.Attempts)
}

// IsTipsSeenContendedError reports whether err's tree contains a *TipsSeenContendedError.
func IsTipsSeenContendedError(err error) bool {
	_, ok := errors.AsType[*TipsSeenContendedError](err)
	return ok
}

// SuspendedError refuses a suspended user on every surface.
type SuspendedError struct {
	Contact string
}

func (e *SuspendedError) Error() string { return "user: suspended" }

// ToAppError maps SuspendedError to USR008 PermissionDenied.
func (e *SuspendedError) ToAppError() *apperror.AppError {
	msg := "This account is suspended."
	if e.Contact != "" {
		msg = "This account is suspended. Contact " + e.Contact + "."
	}
	return apperror.New(apperror.CodeUserSuspended, msg, codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodeUserSuspended})
}

// IsSuspendedError reports whether err's tree contains a *SuspendedError.
func IsSuspendedError(err error) bool {
	_, ok := errors.AsType[*SuspendedError](err)
	return ok
}

// StaffProtectedError refuses suspending platform staff, the caller included.
type StaffProtectedError struct{}

func (*StaffProtectedError) Error() string { return "user: staff cannot be suspended" }

// ToAppError maps StaffProtectedError to USR009 FailedPrecondition.
func (*StaffProtectedError) ToAppError() *apperror.AppError {
	return apperror.New(apperror.CodeUserStaffProtected, "Staff can't be suspended here. Remove them from the system org first.",
		codes.FailedPrecondition, &apperrorv1.ErrorDetail{Code: apperror.CodeUserStaffProtected})
}

// IsStaffProtectedError reports whether err's tree contains a *StaffProtectedError.
func IsStaffProtectedError(err error) bool {
	_, ok := errors.AsType[*StaffProtectedError](err)
	return ok
}

// MembershipMissingError signals that the user holds no membership in the org.
type MembershipMissingError struct{}

func (*MembershipMissingError) Error() string { return "user: membership missing" }

// IsMembershipMissingError reports whether err's tree contains a *MembershipMissingError.
func IsMembershipMissingError(err error) bool {
	_, ok := errors.AsType[*MembershipMissingError](err)
	return ok
}
