package category_test

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"altalune.id/yasaku/internal/category"
	"altalune.id/yasaku/internal/i18n"
)

func TestDefaultsShape(t *testing.T) {
	require.Len(t, category.Defaults, 20)

	var expense, income int
	seen := map[string]bool{}
	for i, d := range category.Defaults {
		assert.NotEmpty(t, d.Key, "Defaults[%d] has no key", i)
		assert.False(t, seen[d.Key], "duplicate default key %q", d.Key)
		seen[d.Key] = true

		switch d.Kind {
		case category.KindExpense:
			expense++
			assert.Zero(t, income, "every expense default must precede the income block (display order)")
		case category.KindIncome:
			income++
		default:
			t.Fatalf("Defaults[%d] (%s) has kind %q", i, d.Key, d.Kind)
		}

		assert.Contains(t, category.AllowedIcons, d.Icon, "default %q uses icon %q outside AllowedIcons", d.Key, d.Icon)
		assert.Contains(t, []string{"chart-1", "chart-2", "chart-3", "chart-4", "chart-5"}, d.Color,
			"default %q uses colour %q outside the chart cycle", d.Key, d.Color)
	}
	assert.Equal(t, 13, expense)
	assert.Equal(t, 7, income)
}

func TestDefaultsEveryKeyResolvesInEveryLocale(t *testing.T) {
	bundle := i18n.NewEmbeddedBundle(i18n.EnUS)
	locales := bundle.All()
	require.Len(t, locales, 5, "expected five locales, got %v", locales)

	messages := map[i18n.Locale]map[string]string{}
	for _, loc := range locales {
		messages[loc] = readLocaleFile(t, loc)
	}

	for _, d := range category.Defaults {
		key := category.DefaultNameKey(d.Key)
		for _, loc := range locales {
			val, ok := messages[loc][key]
			assert.True(t, ok, "locale %s is missing %q", loc, key)
			if loc == i18n.EnUS || loc == i18n.IdID {
				assert.NotEmpty(t, val, "locale %s must carry a real value for %q", loc, key)
			}
		}
	}
}

func readLocaleFile(t *testing.T, loc i18n.Locale) map[string]string {
	t.Helper()
	name := "locales/" + i18n.LocaleFilenamePrefix + string(loc) + i18n.LocaleFilenameSuffix
	buf, err := fs.ReadFile(i18n.EmbeddedLocalesFS(), name)
	require.NoError(t, err, "reading %s", name)

	var raw map[string]any
	require.NoError(t, yaml.Unmarshal(buf, &raw), "parsing %s", name)

	out := make(map[string]string, len(raw))
	for k, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}

func TestDefaultsCarryTheExpectedEnglishAndIndonesianNames(t *testing.T) {
	en := readLocaleFile(t, i18n.EnUS)
	id := readLocaleFile(t, i18n.IdID)

	want := []struct{ key, en, id string }{
		{"food", "Food & Drinks", "Makan & Minum"},
		{"transport", "Transport", "Transportasi"},
		{"shopping", "Shopping", "Belanja"},
		{"bills", "Bills & Utilities", "Tagihan & Utilitas"},
		{"phone_internet", "Phone & Internet", "Pulsa & Internet"},
		{"health", "Health", "Kesehatan"},
		{"home", "Home", "Rumah"},
		{"entertainment", "Entertainment", "Hiburan"},
		{"education", "Education", "Pendidikan"},
		{"family", "Family", "Keluarga"},
		{"installments", "Installments", "Cicilan"},
		{"charity", "Zakat & Charity", "Zakat & Sedekah"},
		{"other_expense", "Other", "Lainnya"},
		{"salary", "Salary", "Gaji"},
		{"bonus", "Bonus & THR", "Bonus & THR"},
		{"freelance", "Freelance", "Freelance"},
		{"business", "Business", "Usaha"},
		{"investment", "Investment", "Investasi"},
		{"gift", "Gift", "Hadiah"},
		{"other_income", "Other", "Lainnya"},
	}
	require.Len(t, want, len(category.Defaults))

	for i, w := range want {
		assert.Equal(t, w.key, category.Defaults[i].Key, "Defaults[%d] is out of display order", i)
		key := category.DefaultNameKey(w.key)
		assert.Equal(t, w.en, en[key])
		assert.Equal(t, w.id, id[key])
	}
}
