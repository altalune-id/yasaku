package auth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/auth"
)

type stampRecorder struct {
	ids []uuid.UUID
	err error
}

func (s *stampRecorder) stamp(_ context.Context, id uuid.UUID) error {
	s.ids = append(s.ids, id)
	return s.err
}

func TestSignIn_StampsLastSignIn(t *testing.T) {
	t.Parallel()
	genesis := auth.Genesis{Email: "admin@example.com", PasswordHash: "root", Name: "Root"}
	local := auth.NewLocalLogin(newFakeUserStore(), genesis, newTestLogger(), noopUnexpected(),
		auth.WithLocalVerifier(exactVerifier),
		auth.WithLocalNotFound(func(err error) bool { return errors.As(err, new(notFoundErr)) }),
	)
	u := &auth.UserRef{ID: uuid.New(), Email: "alice@example.com", Name: "Alice", Source: "oidc"}
	oidc := auth.NewOIDCLogin(
		func(context.Context, auth.EnsureClaims) (*auth.UserRef, bool, error) { return u, false, nil },
		nil, newTestLogger(), noopUnexpected(),
	)

	t.Run("local", func(t *testing.T) {
		t.Parallel()
		rec := &stampRecorder{}
		s := auth.NewService(local, nil, newTestLogger(), noopUnexpected(), auth.WithSignInStamp(rec.stamp))
		p, err := s.LoginLocal(context.Background(), auth.Credentials{Email: "admin@example.com", Password: "root"})
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{p.UserID}, rec.ids)
	})

	t.Run("oidc", func(t *testing.T) {
		t.Parallel()
		rec := &stampRecorder{}
		s := auth.NewService(nil, oidc, newTestLogger(), noopUnexpected(), auth.WithSignInStamp(rec.stamp))
		_, err := s.LoginOIDC(context.Background(), auth.OIDCClaims{Issuer: "https://idp", Subject: "sub", Email: u.Email})
		require.NoError(t, err)
		assert.Equal(t, []uuid.UUID{u.ID}, rec.ids)
	})

	t.Run("failed sign-in stamps nothing", func(t *testing.T) {
		t.Parallel()
		rec := &stampRecorder{}
		s := auth.NewService(local, nil, newTestLogger(), noopUnexpected(), auth.WithSignInStamp(rec.stamp))
		_, err := s.LoginLocal(context.Background(), auth.Credentials{Email: "admin@example.com", Password: "wrong"})
		require.Error(t, err)
		assert.Empty(t, rec.ids)
	})

	t.Run("a failed stamp never blocks sign-in", func(t *testing.T) {
		t.Parallel()
		rec := &stampRecorder{err: errors.New("db down")}
		s := auth.NewService(nil, oidc, newTestLogger(), noopUnexpected(), auth.WithSignInStamp(rec.stamp))
		_, err := s.LoginOIDC(context.Background(), auth.OIDCClaims{Issuer: "https://idp", Subject: "sub", Email: u.Email})
		require.NoError(t, err)
		assert.Len(t, rec.ids, 1)
	})
}

func TestSignIn_GateRefusesBeforeStamp(t *testing.T) {
	t.Parallel()
	u := &auth.UserRef{ID: uuid.New(), Email: "sus@example.com", Name: "Sus", Source: "oidc"}
	oidc := auth.NewOIDCLogin(
		func(context.Context, auth.EnsureClaims) (*auth.UserRef, bool, error) { return u, false, nil },
		nil, newTestLogger(), noopUnexpected(),
	)
	refused := errors.New("suspended")
	rec := &stampRecorder{}
	s := auth.NewService(nil, oidc, newTestLogger(), noopUnexpected(),
		auth.WithSignInGate(func(_ context.Context, id uuid.UUID) error {
			assert.Equal(t, u.ID, id)
			return refused
		}),
		auth.WithSignInStamp(rec.stamp))
	p, err := s.LoginOIDC(context.Background(), auth.OIDCClaims{Issuer: "https://idp", Subject: "sub", Email: u.Email})
	require.ErrorIs(t, err, refused)
	assert.Equal(t, uuid.Nil, p.UserID, "a refused sign-in yields no principal")
	assert.Empty(t, rec.ids, "a refused sign-in is not stamped")
}
