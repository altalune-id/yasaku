package authz

import (
	"errors"
	"sync"

	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/i18n"
)

// Side names which half of the role-and-scope intersection refused.
type Side string

const (
	// SideRole means the member's role does not hold the permission.
	SideRole Side = "role"
	// SideScope means the token or key's scopes do not admit it.
	SideScope Side = "scope"
)

//nolint:gochecknoglobals // the embedded locale bundle, built once and read-only.
var bundle = sync.OnceValue(func() *i18n.Bundle { return i18n.NewEmbeddedBundle(i18n.EnUS) })

//i18n:use authz.*

// DeniedError refuses a call the bound principal does not hold the permission for.
type DeniedError struct {
	Permission Permission
	Org        string
	Side       Side
	Scopes     []string
}

func (e *DeniedError) Error() string {
	return "authz: " + string(e.Permission) + " denied (" + string(e.Side) + ")"
}

// ToAppError maps DeniedError to ROL001 PermissionDenied, worded for whoever reads it, an AI client included.
func (e *DeniedError) ToAppError() *apperror.AppError {
	return apperror.New(apperror.CodePermissionDenied, e.message(), codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodePermissionDenied, Meta: map[string]string{"permission": string(e.Permission), "side": string(e.Side)}})
}

func (e *DeniedError) message() string {
	t := bundle().For(i18n.EnUS)
	d, _ := Lookup(e.Permission)
	action := t.T(d.LabelKey)
	switch {
	case e.Side == SideRole:
		return t.T("authz.denied_role", "Org", e.Org, "Action", action)
	case d.Holder == HolderConsole:
		return t.T("authz.denied_console", "Action", action)
	default:
		return t.T("authz.denied_scope", "Granted", t.T(granted(e.Scopes)))
	}
}

// IsDeniedError reports whether err's tree contains a *DeniedError.
func IsDeniedError(err error) bool {
	_, ok := errors.AsType[*DeniedError](err)
	return ok
}

// EscalationOp names what an escalation refusal was about.
type EscalationOp string

const (
	// EscalationGrant refuses giving a role.
	EscalationGrant EscalationOp = ""
	// EscalationRemove refuses removing a member.
	EscalationRemove EscalationOp = "remove"
)

// EscalationError refuses giving a role, or removing a member whose role, that holds permissions the caller's own role does not.
type EscalationError struct {
	Role string
	Op   EscalationOp
}

func (e *EscalationError) Error() string { return "authz: role " + e.Role + " escalates the caller" }

// ToAppError maps EscalationError to ROL004 PermissionDenied.
func (e *EscalationError) ToAppError() *apperror.AppError {
	key := "authz.escalation"
	if e.Op == EscalationRemove {
		key = "authz.escalation_remove"
	}
	return apperror.New(apperror.CodeRoleEscalation, bundle().For(i18n.EnUS).T(key, "Role", e.Role), codes.PermissionDenied,
		&apperrorv1.ErrorDetail{Code: apperror.CodeRoleEscalation, Meta: map[string]string{"role": e.Role}})
}

// IsEscalationError reports whether err's tree contains a *EscalationError.
func IsEscalationError(err error) bool {
	_, ok := errors.AsType[*EscalationError](err)
	return ok
}
