package templates

import (
	"regexp"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/web"
)

var toWalletSelect = regexp.MustCompile(`<select id="tx-to-wallet"[^>]*>`)

func TestTransactionForm_ToWalletRequiredOnlyForTransfer(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	for kind, want := range map[string]bool{"transfer": true, "expense": false, "income": false} {
		v := sheetFormView()
		v.Kind = kind
		tag := toWalletSelect.FindString(render(t, TransactionForm(d, v)))
		if tag == "" {
			t.Fatalf("%s: no To wallet select", kind)
		}
		if got := regexp.MustCompile(`\srequired[\s>]`).MatchString(tag); got != want {
			t.Errorf("%s: required = %v, want %v in %s", kind, got, want, tag)
		}
	}
}

func TestTransactionForm_ToWalletErrorMarksTheSelect(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	v := sheetFormView()
	v.Kind = "transfer"
	v.ToWalletError = "Pick a different wallet"
	got := render(t, TransactionForm(d, v))

	requireContains(t, toWalletSelect.FindString(got), `aria-invalid="true"`, `aria-describedby="tx-to-wallet-hint"`)
	requireContains(t, got, `<p id="tx-to-wallet-hint" class="mt-1 text-xs text-error">Pick a different wallet</p>`)
}

func TestTransactionForm_DateErrorShowsUnderTheChipsInBothModes(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	v := sheetFormView()
	v.DateError = "November is closed."
	got := render(t, TransactionForm(d, v))

	requireContains(t, regexp.MustCompile(`<input[^>]*id="tx-date"[^>]*>`).FindString(got), `aria-invalid="true"`, `aria-describedby="tx-date-error"`)
	requireContains(t, regexp.MustCompile(`<button[^>]*data-dialog-open="tx-date-dialog"[^>]*>`).FindString(got), `aria-invalid="true"`, `aria-describedby="tx-date-error"`)
	requireContains(t, got, `<p id="tx-date-error" class="mt-1 text-xs text-error">November is closed.</p>`)
	if n := strings.Count(got, "November is closed."); n != 1 {
		t.Errorf("date error rendered %d times, want 1", n)
	}
}

func TestTransactionForm_FocusGoesToTheInvalidToWallet(t *testing.T) {
	d := web.LayoutData{ActiveOrg: &web.ActiveOrg{Slug: "acme"}}
	amount := regexp.MustCompile(`<input[^>]*id="tx-amount"[^>]*>`)
	autofocus := regexp.MustCompile(`\sautofocus[\s>]`)
	v := sheetFormView()
	v.Kind = "transfer"

	clean := render(t, TransactionForm(d, v))
	if !autofocus.MatchString(amount.FindString(clean)) || autofocus.MatchString(toWalletSelect.FindString(clean)) {
		t.Error("without an error the amount takes focus")
	}

	v.ToWalletError = "Choose the wallet the money goes to."
	bad := render(t, TransactionForm(d, v))
	if autofocus.MatchString(amount.FindString(bad)) || !autofocus.MatchString(toWalletSelect.FindString(bad)) {
		t.Error("a To wallet error moves focus to the To wallet")
	}
}
