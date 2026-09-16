package wallet_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/wallet"
	"altalune.id/yasaku/money"
)

func params() wallet.Params {
	return wallet.Params{Name: "BCA", Kind: wallet.KindBank, Provider: "Bank Central Asia", Currency: money.IDR}
}

func TestNew(t *testing.T) {
	orgID, projectID := uuid.New(), uuid.New()

	t.Run("trims name and provider", func(t *testing.T) {
		p := params()
		p.Name = "  BCA  "
		p.Provider = "  Bank Central Asia  "
		w, err := wallet.New(orgID, projectID, p)
		require.NoError(t, err)
		assert.Equal(t, "BCA", w.Name)
		assert.Equal(t, "Bank Central Asia", w.Provider)
		assert.Equal(t, orgID, w.OrgID)
		assert.Equal(t, projectID, w.ProjectID)
		assert.Equal(t, money.IDR, w.Currency)
		assert.NotEqual(t, uuid.Nil, w.ID)
		assert.False(t, w.IsArchived())
		assert.Equal(t, w.CreatedAt, w.UpdatedAt)
	})

	t.Run("rejects an empty name", func(t *testing.T) {
		p := params()
		p.Name = "   "
		_, err := wallet.New(orgID, projectID, p)
		assert.True(t, wallet.IsInvalidNameError(err), "got %T: %v", err, err)
	})

	t.Run("accepts 80 runes and rejects 81", func(t *testing.T) {
		p := params()
		p.Name = strings.Repeat("é", wallet.MaxNameRunes)
		_, err := wallet.New(orgID, projectID, p)
		require.NoError(t, err)

		p.Name = strings.Repeat("é", wallet.MaxNameRunes+1)
		_, err = wallet.New(orgID, projectID, p)
		assert.True(t, wallet.IsInvalidNameError(err), "got %T: %v", err, err)
	})

	t.Run("rejects an unknown kind", func(t *testing.T) {
		p := params()
		p.Kind = wallet.Kind("crypto")
		_, err := wallet.New(orgID, projectID, p)
		assert.True(t, wallet.IsInvalidKindError(err), "got %T: %v", err, err)
	})

	t.Run("rejects an unknown currency", func(t *testing.T) {
		p := params()
		p.Currency = money.Currency("XXX")
		_, err := wallet.New(orgID, projectID, p)
		assert.True(t, money.IsUnknownCurrencyError(err), "got %T: %v", err, err)

		var unknown *money.UnknownCurrencyError
		require.ErrorAs(t, err, &unknown)
		assert.Equal(t, "XXX", unknown.Code)
	})

	t.Run("keeps the caller's exclude flag", func(t *testing.T) {
		p := params()
		p.ExcludeFromTotal = true
		w, err := wallet.New(orgID, projectID, p)
		require.NoError(t, err)
		assert.True(t, w.ExcludeFromTotal)
	})
}

func TestParseKind(t *testing.T) {
	for _, s := range []string{"cash", "bank", "ewallet", "savings", "investment", "other"} {
		got, err := wallet.ParseKind(s)
		require.NoError(t, err)
		assert.Equal(t, wallet.Kind(s), got)
	}

	got, err := wallet.ParseKind("  BANK ")
	require.NoError(t, err)
	assert.Equal(t, wallet.KindBank, got)

	_, err = wallet.ParseKind("crypto")
	assert.True(t, wallet.IsInvalidKindError(err), "got %T: %v", err, err)
}

func TestKindDefaultExcludeFromTotal(t *testing.T) {
	excluded := map[wallet.Kind]bool{wallet.KindSavings: true, wallet.KindInvestment: true}
	for _, k := range []wallet.Kind{
		wallet.KindCash, wallet.KindBank, wallet.KindEwallet,
		wallet.KindSavings, wallet.KindInvestment, wallet.KindOther,
	} {
		assert.Equal(t, excluded[k], k.DefaultExcludeFromTotal(), "kind %q", k)
	}
}

func TestRename(t *testing.T) {
	w, err := wallet.New(uuid.New(), uuid.New(), params())
	require.NoError(t, err)

	require.NoError(t, w.Rename("  Mandiri  "))
	assert.Equal(t, "Mandiri", w.Name)

	assert.True(t, wallet.IsInvalidNameError(w.Rename("  ")))
	assert.True(t, wallet.IsInvalidNameError(w.Rename(strings.Repeat("x", wallet.MaxNameRunes+1))))
	assert.Equal(t, "Mandiri", w.Name, "a refused rename must not mutate the aggregate")
}

func TestUpdate(t *testing.T) {
	t.Run("changes kind, provider and exclude flag", func(t *testing.T) {
		w, err := wallet.New(uuid.New(), uuid.New(), params())
		require.NoError(t, err)

		require.NoError(t, w.Update(wallet.KindSavings, "  BCA Tahapan  ", true))
		assert.Equal(t, wallet.KindSavings, w.Kind)
		assert.Equal(t, "BCA Tahapan", w.Provider)
		assert.True(t, w.ExcludeFromTotal)
	})

	t.Run("rejects an unknown kind", func(t *testing.T) {
		w, err := wallet.New(uuid.New(), uuid.New(), params())
		require.NoError(t, err)
		assert.True(t, wallet.IsInvalidKindError(w.Update(wallet.Kind("crypto"), "", false)))
		assert.Equal(t, wallet.KindBank, w.Kind)
	})

	t.Run("refuses an archived wallet", func(t *testing.T) {
		w, err := wallet.New(uuid.New(), uuid.New(), params())
		require.NoError(t, err)
		w.Archive()
		assert.True(t, wallet.IsArchivedError(w.Update(wallet.KindCash, "", false)), "got %T", err)
	})
}

// TestRenameAllowsArchived pins the escape from a name collision: an archived wallet whose name
// has since been taken by a new active wallet can only be unarchived after it is renamed, so
// Rename must stay available while archived. Update is the operation ArchivedError guards.
func TestRenameAllowsArchived(t *testing.T) {
	w, err := wallet.New(uuid.New(), uuid.New(), params())
	require.NoError(t, err)
	w.Archive()
	require.True(t, w.IsArchived())

	require.NoError(t, w.Rename("BCA (closed)"))
	assert.Equal(t, "BCA (closed)", w.Name)
	assert.True(t, w.IsArchived(), "a rename must not resurrect an archived wallet")

	assert.True(t, wallet.IsInvalidNameError(w.Rename("  ")), "validation still applies while archived")
}

func TestArchiveUnarchive(t *testing.T) {
	w, err := wallet.New(uuid.New(), uuid.New(), params())
	require.NoError(t, err)
	assert.False(t, w.IsArchived())

	w.Archive()
	require.True(t, w.IsArchived())
	first := *w.ArchivedAt

	w.Archive()
	assert.True(t, w.IsArchived())
	assert.Equal(t, first, *w.ArchivedAt, "Archive must be idempotent")

	w.Unarchive()
	assert.False(t, w.IsArchived())
	assert.Nil(t, w.ArchivedAt)

	before := w.UpdatedAt
	w.Unarchive()
	assert.False(t, w.IsArchived())
	assert.Equal(t, before, w.UpdatedAt, "Unarchive must be idempotent")
}
