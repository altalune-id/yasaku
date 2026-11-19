package templates

import (
	"strings"
	"testing"
	"time"

	"altalune.id/yasaku/civil"
	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func sheetsView(state string) OpensheetView {
	at := time.Date(2026, 11, 18, 3, 0, 0, 0, time.UTC)
	return OpensheetView{
		ProjectSlug: "home", CanManage: true, Linked: true, Enabled: state != "off_manual" && state != "paused", Verified: true,
		State: OpensheetState(state), LastError: "boom (GEN002)", ReasonKey: "opensheet.reason.key_rejected",
		AutoDisabledAt: &at, Failing: 1, GivenUp: 2, Pending: 4,
		Form: OpensheetForm{OSOrg: "acme", OSProject: "books"},
	}
}

func TestOpensheetStatus_EveryStateHasAHeadlineAndItsBanner(t *testing.T) {
	d := web.LayoutData{}
	banners := map[string][]string{
		"paused":     {"data-opensheet-auto-disabled", "opensheet.state.paused_title", "opensheet.state.paused_fix", "boom (GEN002)", "opensheet.reason.key_rejected"},
		"off_manual": {"opensheet.state.off_manual_body"},
		"failing":    {"data-opensheet-failing", "opensheet.state.failing_body", "opensheet.state.rows_never_pause", "opensheet.state.retry_soon", "data-opensheet-given-up"},
		"given_up":   {"data-opensheet-given-up", "opensheet.state.given_up_body"},
		"first":      {"data-opensheet-awaiting-first-sync", "opensheet.state.first_body", "boom (GEN002)"},
		"pending":    nil,
		"ok":         nil,
	}
	for state, want := range banners {
		t.Run(state, func(t *testing.T) {
			got := render(t, OpensheetStatus(d, sheetsView(state)))
			requireContains(t, got, `>opensheet.state.`+state+`</span>`, `data-opensheet-state="`+state+`"`)
			requireContains(t, got, want...)
			if want == nil && strings.Contains(got, `class="alert alert-soft alert-`) {
				t.Errorf("%s shows a banner", state)
			}
			if strings.Contains(got, ">GEN002<") || strings.Contains(got, "opensheet.state.\"") {
				t.Errorf("%s puts a raw code or an empty key in a headline", state)
			}
		})
	}
}

func TestOpensheetStatus_StreakOnlyFromLinkFailures(t *testing.T) {
	v := sheetsView("failing")
	v.Streak = 2
	got := render(t, OpensheetStatus(web.LayoutData{}, v))
	requireContains(t, got, `data-opensheet-streak="2"`, "opensheet.state.streak")
	if strings.Contains(got, "opensheet.state.rows_never_pause") {
		t.Error("a link streak also says rows never pause")
	}
}

func TestOpensheetStatus_PausedTimeIsABrowserTime(t *testing.T) {
	en := web.LayoutData{}
	v := sheetsView("paused")
	got := render(t, opensheetTimed(en, "Paused on "+opensheetTimeMark+" after 3.", v.AutoDisabledAt))
	requireContains(t, got, `Paused on <time datetime="2026-11-18T03:00:00Z" data-datetime>`, "</time> after 3.")
}

func TestOpensheetStatus_MemberSeesNoControls(t *testing.T) {
	v := sheetsView("ok")
	v.CanManage = false
	got := render(t, OpensheetStatus(web.LayoutData{}, v))
	requireContains(t, got, "data-opensheet-read-only", "opensheet.member_hint")
	for _, banned := range []string{`type="checkbox"`, "/opensheet/sync", OpensheetRemoveDialogID, `<button type="submit" class="btn btn-primary">opensheet.save`} {
		if strings.Contains(got, banned) {
			t.Errorf("member sees %s", banned)
		}
	}
}

func TestOpensheetStatus_ToggleOffWhenPausedAndPostsFalseWhenUnchecked(t *testing.T) {
	got := render(t, OpensheetStatus(web.LayoutData{}, sheetsView("paused")))
	requireContains(t, got, `name="enabled" value="true" data-opensheet-enabled="false"`, `<input type="hidden" name="enabled" value="false">`, "opensheet.sync_state.paused", `hx-trigger="change"`)
	if strings.Contains(got, `value="true" checked`) {
		t.Error("paused toggle is on")
	}
}

func TestOpensheetLayout_RemoveUsesTheKitConfirm(t *testing.T) {
	got := render(t, OpensheetLayout(web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}, sheetsView("ok")))
	requireContains(t, got, `data-dialog-open="`+OpensheetRemoveDialogID+`"`, `id="`+OpensheetRemoveDialogID+`"`, `hx-post="/orgs/acme/projects/home/opensheet/delete"`)
	if strings.Contains(got, "hx-confirm") {
		t.Error("remove still uses hx-confirm")
	}
}

func TestOpensheetStatus_PausedMemberIsToldToAskAManager(t *testing.T) {
	v := sheetsView("paused")
	v.CanManage = false
	got := render(t, OpensheetStatus(web.LayoutData{}, v))
	requireContains(t, got, "opensheet.member_hint")
	if strings.Contains(got, "opensheet.state.paused_fix") {
		t.Error("member gets the manager's fix steps")
	}
}

func TestOpensheetRows_ShowTheKindAndTheLabelWhenKnown(t *testing.T) {
	v := sheetsView("failing")
	v.Rows = []OpensheetRowError{
		{EntityKey: "opensheet.tab.transaction", Label: "Kopi", ReasonKey: "opensheet.reason.row_refused"},
		{EntityKey: "opensheet.tab.wallet"},
	}
	got := render(t, OpensheetStatus(web.LayoutData{}, v))
	requireContains(t, got, `<span class="font-medium break-words">opensheet.tab.transaction</span> <span class="break-words">Kopi</span>`, `<span class="font-medium break-words">opensheet.tab.wallet</span>`, "opensheet.reason.row_refused")
}

func TestOpensheetStatus_RedactedMemberViewKeepsThePlainReason(t *testing.T) {
	for _, state := range []OpensheetState{OpensheetPaused, OpensheetFailing, OpensheetFirst} {
		v := sheetsView(string(state))
		v.CanManage, v.LastError, v.SyncErrorCode = false, "", ""
		v.Rows = []OpensheetRowError{{EntityKey: "opensheet.tab.wallet", ReasonKey: "opensheet.reason.row_refused"}}
		got := render(t, OpensheetStatus(web.LayoutData{}, v))
		requireContains(t, got, "opensheet.reason.key_rejected", "opensheet.reason.row_refused")
		if strings.Contains(got, "data-opensheet-status-last-error") || strings.Contains(got, "opensheet.details") {
			t.Errorf("%s: redacted view still renders a details line", state)
		}
	}
}

func TestOpensheetRows_ReasonGetsTheRetryLimit(t *testing.T) {
	d := web.LayoutData{Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)}
	v := sheetsView(string(OpensheetFailing))
	v.Rows = []OpensheetRowError{{EntityKey: "opensheet.tab.wallet", ReasonKey: "opensheet.reason.row_refused"}}
	got := render(t, OpensheetStatus(d, v))
	if strings.Contains(got, "no value") {
		t.Error("a reason line misses its template params")
	}
}

func TestOpensheetRows_DateIsALedgerDayNotAnInstant(t *testing.T) {
	day := civil.Date{Year: 2026, Month: time.October, Day: 5}
	v := sheetsView(string(OpensheetFailing))
	v.Rows = []OpensheetRowError{{EntityKey: "opensheet.tab.transaction", Label: "Kopi", OccurredOn: &day}}
	got := render(t, OpensheetStatus(web.LayoutData{}, v))
	requireContains(t, got, `<time datetime="2026-10-05" data-ledger-date data-date-style="compact">5 Oct</time>`)
	row := got[strings.Index(got, `data-opensheet-row="opensheet.tab.transaction"`):]
	if row = row[:strings.Index(row, "</li>")]; strings.Contains(row, "data-datetime") {
		t.Error("row date renders as an instant")
	}
}

func TestOpensheetStatus_ConnectionIsTwoWrappingLines(t *testing.T) {
	v := sheetsView(string(OpensheetOK))
	v.KeyHint = "7Q2c"
	got := render(t, OpensheetStatus(web.LayoutData{}, v))
	requireContains(t, got, `<p class="min-w-0 text-sm break-words">opensheet.connected_target</p>`, `<p class="min-w-0 text-sm break-words">opensheet.key_ending</p>`)
	if strings.Contains(got, "opensheet.connected_to") || strings.Contains(got, "truncate text-sm") {
		t.Error("connection still one truncating line")
	}
}
