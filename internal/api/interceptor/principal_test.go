package interceptor_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	"altalune.id/yasaku/internal/api/interceptor"
	"altalune.id/yasaku/internal/apperror"
	"altalune.id/yasaku/internal/platform/session"
	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

const (
	idpIssuer  = "https://idp.test"
	idpSubject = "sub-42"
)

func seedIDPUser(t *testing.T) (*fakes.User, *user.User) {
	t.Helper()
	store := fakes.NewUser()
	u, err := user.New("known@example.com", "Known", user.SourceOIDC)
	if err != nil {
		t.Fatalf("user.New: %v", err)
	}
	u.IDPIssuer = idpIssuer
	u.IDPSubject = idpSubject
	if err := store.Save(context.Background(), u); err != nil {
		t.Fatalf("Save: %v", err)
	}
	return store, u
}

func TestPrincipal_ResolvesKnownSubject(t *testing.T) {
	store, u := seedIDPUser(t)
	inter := interceptor.Principal(store)

	var got session.Principal
	next := connect.UnaryFunc(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		got = session.PrincipalFrom(ctx)
		return connect.NewResponse(&struct{}{}), nil
	})
	ctx := session.PrincipalInto(context.Background(), session.Principal{
		IDPIssuer:  idpIssuer,
		IDPSubject: idpSubject,
	})
	if _, err := inter(next)(ctx, newReq()); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if got.UserID != u.ID {
		t.Errorf("UserID = %v, want %v", got.UserID, u.ID)
	}
	if got.Email != u.Email {
		t.Errorf("Email = %q, want %q", got.Email, u.Email)
	}
}

func TestPrincipal_UnknownSubjectIsPermissionDenied(t *testing.T) {
	store, _ := seedIDPUser(t)
	// The server chains Wrap outside Principal; compose the same way so the wire code is real.
	wrap, inter := interceptor.Wrap(nil), interceptor.Principal(store)

	next := connect.UnaryFunc(func(context.Context, connect.AnyRequest) (connect.AnyResponse, error) {
		t.Fatal("handler must not run for an unknown subject")
		return nil, nil
	})
	ctx := session.PrincipalInto(context.Background(), session.Principal{
		IDPIssuer:  idpIssuer,
		IDPSubject: "sub-nobody",
	})
	_, err := wrap(inter(next))(ctx, newReq())
	if err == nil {
		t.Fatal("expected an error")
	}
	if got := connect.CodeOf(err); got != connect.CodePermissionDenied {
		t.Errorf("code = %v, want PermissionDenied", got)
	}
	ae, ok := apperror.AsAppError(err)
	if !ok {
		t.Fatalf("error is not an AppError: %v", err)
	}
	if ae.Code() != apperror.CodeMCPUnknownUser {
		t.Errorf("code = %q, want %q", ae.Code(), apperror.CodeMCPUnknownUser)
	}
}

// SECURITY: identity resolution must not escalate authorization, so the stored row is assigned,
// never OR-ed into whatever the token claimed.
func TestPrincipal_TakesIsAdminFromTheStoredUserNotTheToken(t *testing.T) {
	store, _ := seedIDPUser(t)
	inter := interceptor.Principal(store)

	var got session.Principal
	next := connect.UnaryFunc(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
		got = session.PrincipalFrom(ctx)
		return connect.NewResponse(&struct{}{}), nil
	})
	ctx := session.PrincipalInto(context.Background(), session.Principal{
		IDPIssuer:  idpIssuer,
		IDPSubject: idpSubject,
		IsAdmin:    true,
	})
	if _, err := inter(next)(ctx, newReq()); err != nil {
		t.Fatalf("interceptor: %v", err)
	}
	if got.IsAdmin {
		t.Error("IsAdmin must come from the stored user, so a token claim cannot survive it")
	}
}

func TestPrincipal_SkipsWhenAlreadyResolvedOrAnonymous(t *testing.T) {
	store, _ := seedIDPUser(t)
	inter := interceptor.Principal(store)

	existing := uuid.New()
	cases := map[string]session.Principal{
		"already resolved": {UserID: existing, IDPIssuer: idpIssuer, IDPSubject: "sub-nobody"},
		"no subject":       {},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			var got session.Principal
			next := connect.UnaryFunc(func(ctx context.Context, _ connect.AnyRequest) (connect.AnyResponse, error) {
				got = session.PrincipalFrom(ctx)
				return connect.NewResponse(&struct{}{}), nil
			})
			ctx := session.PrincipalInto(context.Background(), p)
			if _, err := inter(next)(ctx, newReq()); err != nil {
				t.Fatalf("interceptor: %v", err)
			}
			if got.UserID != p.UserID {
				t.Errorf("UserID = %v, want %v", got.UserID, p.UserID)
			}
		})
	}
}
