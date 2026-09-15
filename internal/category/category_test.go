package category_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"altalune.id/yasaku/internal/category"
)

func TestParseKind(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		want  category.Kind
		valid bool
	}{
		{"expense", "expense", category.KindExpense, true},
		{"income", "income", category.KindIncome, true},
		{"trims and lowercases", "  Expense ", category.KindExpense, true},
		{"transfer is not a category kind", "transfer", "", false},
		{"empty", "", "", false},
		{"garbage", "nope", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := category.ParseKind(tt.in)
			if !tt.valid {
				assert.True(t, category.IsInvalidKindError(err), "got %T: %v", err, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNew(t *testing.T) {
	orgID, projID := uuid.New(), uuid.New()

	t.Run("happy path trims the name and stamps timestamps", func(t *testing.T) {
		c, err := category.New(orgID, projID, "  Food & Drinks  ", category.KindExpense, "utensils", "chart-1", 3)
		require.NoError(t, err)
		assert.Equal(t, "Food & Drinks", c.Name)
		assert.Equal(t, category.KindExpense, c.Kind)
		assert.Equal(t, "utensils", c.Icon)
		assert.Equal(t, "chart-1", c.Color)
		assert.Equal(t, 3, c.SortOrder)
		assert.Equal(t, orgID, c.OrgID)
		assert.Equal(t, projID, c.ProjectID)
		assert.NotEqual(t, uuid.Nil, c.ID)
		assert.False(t, c.IsArchived())
		assert.WithinDuration(t, time.Now().UTC(), c.CreatedAt, time.Minute)
		assert.Equal(t, c.CreatedAt, c.UpdatedAt)
	})

	t.Run("blank icon and colour are allowed", func(t *testing.T) {
		c, err := category.New(orgID, projID, "Misc", category.KindIncome, "", "", 0)
		require.NoError(t, err)
		assert.Empty(t, c.Icon)
		assert.Empty(t, c.Color)
	})

	t.Run("empty name", func(t *testing.T) {
		_, err := category.New(orgID, projID, "   ", category.KindExpense, "", "", 0)
		assert.True(t, category.IsInvalidNameError(err), "got %T: %v", err, err)
	})

	t.Run("name over the rune cap", func(t *testing.T) {
		long := make([]rune, category.MaxNameRunes+1)
		for i := range long {
			long[i] = 'a'
		}
		_, err := category.New(orgID, projID, string(long), category.KindExpense, "", "", 0)
		assert.True(t, category.IsInvalidNameError(err), "got %T: %v", err, err)
	})

	t.Run("unknown kind", func(t *testing.T) {
		_, err := category.New(orgID, projID, "Food", category.Kind("transfer"), "", "", 0)
		assert.True(t, category.IsInvalidKindError(err), "got %T: %v", err, err)
	})

	t.Run("icon outside the allow-list", func(t *testing.T) {
		_, err := category.New(orgID, projID, "Food", category.KindExpense, "skull", "", 0)
		assert.True(t, category.IsInvalidIconError(err), "got %T: %v", err, err)
	})

	t.Run("colour outside the accepted formats", func(t *testing.T) {
		for _, bad := range []string{"chart-0", "chart-6", "red", "#abc", "#12345g", "123456", "#1234567"} {
			_, err := category.New(orgID, projID, "Food", category.KindExpense, "", bad, 0)
			assert.True(t, category.IsInvalidColorError(err), "colour %q must be refused, got %T: %v", bad, err, err)
		}
	})

	t.Run("every accepted colour form", func(t *testing.T) {
		for _, ok := range []string{"", "chart-1", "chart-2", "chart-3", "chart-4", "chart-5", "#ff8800", "#FF8800"} {
			_, err := category.New(orgID, projID, "Food", category.KindExpense, "", ok, 0)
			require.NoError(t, err, "colour %q must be accepted", ok)
		}
	})

	t.Run("hex colour is normalised to lower case", func(t *testing.T) {
		c, err := category.New(orgID, projID, "Food", category.KindExpense, "", "#FF8800", 0)
		require.NoError(t, err)
		assert.Equal(t, "#ff8800", c.Color)
	})

	t.Run("every allow-listed icon is accepted", func(t *testing.T) {
		for _, icon := range category.AllowedIcons {
			_, err := category.New(orgID, projID, "Food", category.KindExpense, icon, "", 0)
			require.NoError(t, err, "icon %q must be accepted", icon)
		}
	})
}

func TestNew_ClampsSortOrderToTheColumnWidth(t *testing.T) {
	orgID, projID := uuid.New(), uuid.New()

	negative, err := category.New(orgID, projID, "Food", category.KindExpense, "", "", -5)
	require.NoError(t, err)
	assert.Zero(t, negative.SortOrder, "a negative display order is meaningless")

	huge, err := category.New(orgID, projID, "Food", category.KindExpense, "", "", category.MaxSortOrder+1)
	require.NoError(t, err)
	assert.Equal(t, category.MaxSortOrder, huge.SortOrder, "sort_order is an INTEGER column")
	assert.Equal(t, int32(category.MaxSortOrder), category.SortOrder32(huge.SortOrder))
	assert.Zero(t, category.SortOrder32(-1))
}

func TestRename(t *testing.T) {
	newCat := func(t *testing.T) *category.Category {
		t.Helper()
		c, err := category.New(uuid.New(), uuid.New(), "Food", category.KindExpense, "", "", 0)
		require.NoError(t, err)
		c.UpdatedAt = c.UpdatedAt.Add(-time.Hour)
		return c
	}

	t.Run("happy path", func(t *testing.T) {
		c := newCat(t)
		before := c.UpdatedAt
		require.NoError(t, c.Rename("  Groceries "))
		assert.Equal(t, "Groceries", c.Name)
		assert.True(t, c.UpdatedAt.After(before), "Rename must bump UpdatedAt")
	})

	t.Run("empty is refused and leaves the name alone", func(t *testing.T) {
		c := newCat(t)
		assert.True(t, category.IsInvalidNameError(c.Rename("  ")))
		assert.Equal(t, "Food", c.Name)
	})
}

func TestUpdate(t *testing.T) {
	newCat := func(t *testing.T) *category.Category {
		t.Helper()
		c, err := category.New(uuid.New(), uuid.New(), "Food", category.KindExpense, "utensils", "chart-1", 0)
		require.NoError(t, err)
		c.UpdatedAt = c.UpdatedAt.Add(-time.Hour)
		return c
	}

	t.Run("happy path", func(t *testing.T) {
		c := newCat(t)
		before := c.UpdatedAt
		require.NoError(t, c.Update("bus", "#00FF00"))
		assert.Equal(t, "bus", c.Icon)
		assert.Equal(t, "#00ff00", c.Color)
		assert.True(t, c.UpdatedAt.After(before))
	})

	t.Run("clearing both is allowed", func(t *testing.T) {
		c := newCat(t)
		require.NoError(t, c.Update("", ""))
		assert.Empty(t, c.Icon)
		assert.Empty(t, c.Color)
	})

	t.Run("a bad icon leaves both fields alone", func(t *testing.T) {
		c := newCat(t)
		assert.True(t, category.IsInvalidIconError(c.Update("skull", "chart-2")))
		assert.Equal(t, "utensils", c.Icon)
		assert.Equal(t, "chart-1", c.Color)
	})

	t.Run("a bad colour leaves both fields alone", func(t *testing.T) {
		c := newCat(t)
		assert.True(t, category.IsInvalidColorError(c.Update("bus", "puce")))
		assert.Equal(t, "utensils", c.Icon)
		assert.Equal(t, "chart-1", c.Color)
	})
}

func TestArchiveUnarchive(t *testing.T) {
	c, err := category.New(uuid.New(), uuid.New(), "Food", category.KindExpense, "", "", 0)
	require.NoError(t, err)
	require.False(t, c.IsArchived())

	c.Archive()
	require.True(t, c.IsArchived())
	first := *c.ArchivedAt

	c.Archive()
	assert.Equal(t, first, *c.ArchivedAt, "Archive must be idempotent, not re-stamped")

	c.Unarchive()
	assert.False(t, c.IsArchived())
	assert.Nil(t, c.ArchivedAt)

	c.Unarchive()
	assert.False(t, c.IsArchived(), "Unarchive must be idempotent")
}

func TestAllowedIconsIsStable(t *testing.T) {
	assert.NotEmpty(t, category.AllowedIcons)
	seen := map[string]bool{}
	for _, icon := range category.AllowedIcons {
		assert.NotEmpty(t, icon)
		assert.False(t, seen[icon], "duplicate icon %q in AllowedIcons", icon)
		seen[icon] = true
	}
}
