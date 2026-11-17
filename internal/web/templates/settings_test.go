package templates

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func TestSettingsTimezoneLabel(t *testing.T) {
	for tz, want := range map[string]string{
		"Asia/Jakarta":  "Asia/Jakarta (WIB, UTC+7)",
		"Asia/Makassar": "Asia/Makassar (WITA, UTC+8)",
		"Asia/Jayapura": "Asia/Jayapura (WIT, UTC+9)",
		"Asia/Kolkata":  "Asia/Kolkata (IST, UTC+5:30)",
		"UTC":           "UTC",
		"Not/AZone":     "Not/AZone",
	} {
		if got := settingsTimezoneLabel(tz); got != want {
			t.Errorf("settingsTimezoneLabel(%q) = %q, want %q", tz, got, want)
		}
	}
}

func settingsView() SettingsView {
	days := make([]int, 28)
	for i := range days {
		days[i] = i + 1
	}
	return SettingsView{
		OrgSlug: "acme", ProjectSlug: "home", Name: "Home", CanRename: true, CanManage: true,
		Timezone: "Asia/Jakarta", Timezones: []string{"Asia/Jakarta", "UTC"},
		Currency: "IDR", Currencies: []string{"IDR", "USD"},
		PeriodStartDay: 25, StartDays: days,
	}
}

func settingsData() web.LayoutData {
	return web.LayoutData{Nonce: "n0nce", Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)}
}

func TestSettings_MoneyCardLabelsAndOneSavePerForm(t *testing.T) {
	out := render(t, SettingsPage(settingsData(), settingsView()))
	for _, want := range []string{
		`<option value="IDR" selected>Indonesian Rupiah (Rp)</option>`,
		`<option value="USD">US Dollar ($)</option>`,
		`<option value="Asia/Jakarta" selected>Asia/Jakarta (WIB, UTC+7)</option>`,
		`<option value="25" selected>Day 25</option>`,
		`<option value="1">Day 1</option>`,
		`<option value="28">Day 28</option>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("settings lacks %s", want)
		}
	}
	if !strings.Contains(out, `<select id="settings-currency"`) && !strings.Contains(out, `name="currency" class="select`) {
		t.Error("the currency stays an editable select")
	}
	if strings.Contains(out, `type="number"`) {
		t.Error("the month start is a Day N select")
	}
	forms := strings.Count(out, "<form ")
	if forms != 2 || strings.Count(out, `type="submit"`) != forms || strings.Count(out, "data-dirty-form") != forms {
		t.Errorf("forms=%d submits=%d dirty=%d: each form has exactly one Save and the dirty hooks", forms, strings.Count(out, `type="submit"`), strings.Count(out, "data-dirty-form"))
	}
	for _, want := range []string{"data-dirty-only", "data-dirty-cancel", "data-dirty-saving", ">Unsaved changes<", ">Cancel<", ">Saving<"} {
		if !strings.Contains(out, want) {
			t.Errorf("settings lacks the %s state", want)
		}
	}
	if strings.Contains(out, "data-dirty-start") {
		t.Error("a clean page starts clean")
	}
}

func TestSettings_RefusedSubmitStartsDirtySoSaveStaysUsable(t *testing.T) {
	v := settingsView()
	v.Error, v.NameError = "bad", "settings.name_error"
	out := render(t, SettingsPage(settingsData(), v))
	if strings.Count(out, "data-dirty-start") != 2 {
		t.Errorf("both refused forms must start dirty: %d", strings.Count(out, "data-dirty-start"))
	}
}

func TestSettings_MemberSeesNoRenameForm(t *testing.T) {
	v := settingsView()
	v.CanRename = false
	out := render(t, SettingsPage(settingsData(), v))
	if strings.Contains(out, `/rename"`) {
		t.Error("only a manager gets the rename form")
	}
}

func TestSettings_ReadOnlyLedgerHasNoSaveAndDisablesFields(t *testing.T) {
	v := settingsView()
	v.CanManage = false
	out := render(t, SettingsPage(settingsData(), v))
	if strings.Count(out, "disabled") != 3 {
		t.Errorf("want the three ledger selects disabled, got %d", strings.Count(out, "disabled"))
	}
	if strings.Count(out, `type="submit"`) != 1 {
		t.Errorf("only the rename form keeps a Save: %d", strings.Count(out, `type="submit"`))
	}
	if !strings.Contains(out, "Only an owner or admin can change these settings.") {
		t.Error("the read-only card says why")
	}
}
