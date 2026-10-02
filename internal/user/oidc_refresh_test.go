package user_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/testutil/fakes"
	"altalune.id/yasaku/internal/user"
)

func TestEnsureFromOIDC_SubjectMatchRefreshesName(t *testing.T) {
	t.Parallel()
	st := fakes.NewUser()
	svc := user.NewService(st, user.GenesisConfig{}, newTestLogger(), noopUnexpected())
	ctx := context.Background()

	first, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-1", Email: "a@x.id", Name: "Old Name"})
	require.NoError(t, err)

	second, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-1", Email: "a@x.id", Name: "New Name"})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, "New Name", second.Name)
}

func TestEnsureFromOIDC_DifferentSubjectSameIssuerStaysDistinct(t *testing.T) {
	t.Parallel()
	st := fakes.NewUser()
	svc := user.NewService(st, user.GenesisConfig{}, newTestLogger(), noopUnexpected())
	ctx := context.Background()

	a, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-a", Email: "a@x.id", Name: "A"})
	require.NoError(t, err)
	b, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-b", Email: "b@x.id", Name: "B"})
	require.NoError(t, err)
	require.NotEqual(t, a.ID, b.ID)
	require.Equal(t, 2, st.Len())
}

func TestEnsureFromOIDC_RejectsEmailConflictOnRefresh(t *testing.T) {
	t.Parallel()
	st := fakes.NewUser()
	svc := user.NewService(st, user.GenesisConfig{}, newTestLogger(), noopUnexpected())
	ctx := context.Background()

	a := &user.User{
		ID:    uuid.New(),
		Email: "a@x.id",
		Name:  "A",
	}
	require.NoError(t, st.Save(ctx, a))

	_, err := svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-b", Email: "b@x.id", Name: "B"})
	require.NoError(t, err)

	_, err = svc.EnsureFromOIDC(ctx, user.Claims{Issuer: "https://idp", Subject: "sub-b", Email: "a@x.id", Name: "B Updated"})
	require.True(t, user.IsAlreadyExistsError(err), "expected AlreadyExistsError, got %T: %v", err, err)
}
