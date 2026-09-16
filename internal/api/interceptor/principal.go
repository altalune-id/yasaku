package interceptor

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	apperrorv1 "altalune.id/yasaku/gen/go/apperror/v1"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/user"
)

// Principal resolves a token principal's IDP subject to a local user and fills in Principal.UserID.
// NOTE: must run after Tenant, which expects to see a bare token principal and skip it.
func Principal(users user.Store) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			p := session.PrincipalFrom(ctx)
			if p.UserID != uuid.Nil || p.IDPSubject == "" || users == nil {
				return next(ctx, req)
			}
			u, err := users.ByIDP(ctx, p.IDPIssuer, p.IDPSubject)
			if err != nil {
				if user.IsNotFoundError(err) {
					return nil, unknownUser(p.IDPIssuer, p.IDPSubject)
				}
				return nil, err
			}
			p.UserID = u.ID
			if p.Email == "" {
				p.Email = u.Email
			}
			if p.Name == "" {
				p.Name = u.Name
			}
			// SECURITY: assignment, never `||` — identity resolution must not escalate authorization.
			p.IsAdmin = u.IsAdmin
			return next(session.PrincipalInto(ctx, p), req)
		}
	}
}

// SECURITY: the subject is echoed in meta but never the reason it failed to resolve.
func unknownUser(issuer, subject string) error {
	return apperror.New(
		apperror.CodeMCPUnknownUser,
		"No local user is linked to this identity",
		codes.PermissionDenied,
		&apperrorv1.ErrorDetail{
			Code: apperror.CodeMCPUnknownUser,
			Meta: map[string]string{"idp_issuer": issuer, "idp_subject": subject},
		},
	)
}
