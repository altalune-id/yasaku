package templates

import (
	"context"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"altalune.id/yasaku/internal/web"
)

func renderKit(t *testing.T, c templ.Component, children string) string {
	t.Helper()
	ctx := context.Background()
	if children != "" {
		ctx = templ.WithChildren(ctx, templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, children)
			return err
		}))
	}
	var sb strings.Builder
	if err := c.Render(ctx, &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	return sb.String()
}

func requireContainsAll(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Fatalf("render = %q, want it to contain %q", got, want)
		}
	}
}

func TestConfirmModalPostsToTheActionAndCancelCloses(t *testing.T) {
	got := renderKit(t, ConfirmModal(web.LayoutData{Nonce: "n0nce"}, ConfirmModalProps{
		ID:           "tx-delete",
		Title:        "Delete entry?",
		Body:         "This cannot be undone.",
		Action:       "/orgs/a/projects/b/transactions/1/delete",
		ConfirmLabel: "Delete",
	}), `<input type="hidden" name="x" value="y">`)

	requireContainsAll(t, got,
		`<dialog id="tx-delete" class="modal `,
		"Delete entry?",
		"This cannot be undone.",
		`method="post"`,
		`action="/orgs/a/projects/b/transactions/1/delete"`,
		`<input type="hidden" name="x" value="y">`,
		`class="btn btn-error"`,
		">Delete</button>",
	)
	cancel := regexp.MustCompile(`<button type="button" data-dialog-close class="btn btn-ghost">[^<]+</button>`).FindString(got)
	if cancel == "" {
		t.Fatalf("ConfirmModal() = %q, want a type=button [data-dialog-close] cancel", got)
	}
	if strings.Contains(got, "onclick") {
		t.Fatalf("ConfirmModal() must carry no inline handler: %q", got)
	}
}

func TestConfirmModalCustomCancelLabelAndDisabledConfirm(t *testing.T) {
	got := renderKit(t, ConfirmModal(web.LayoutData{}, ConfirmModalProps{
		ID: "c", Title: "T", Action: "/x", ConfirmLabel: "Go", CancelLabel: "Keep it", ConfirmDisabled: true,
	}), "")
	requireContainsAll(t, got, ">Keep it</button>", " disabled")
	if strings.Contains(got, "<p class") {
		t.Fatalf("an empty body must render no paragraph: %q", got)
	}
}

func TestEmptyStateRendersHeadingLevelBodyAndActions(t *testing.T) {
	got := renderKit(t, EmptyState(EmptyStateProps{
		Title: "No wallets yet", Body: "Create one to start.", Icon: "wallet", HeadingLevel: 3,
	}), `<a id="cta" href="/w">Create a wallet</a>`)
	requireContainsAll(t, got,
		`<h3 class="text-xl font-semibold">No wallets yet</h3>`,
		"Create one to start.",
		`<svg`,
		`<a id="cta" href="/w">Create a wallet</a>`,
	)

	plain := renderKit(t, EmptyState(EmptyStateProps{Title: "Nothing"}), "")
	requireContainsAll(t, plain, `<h2 class="text-xl font-semibold">Nothing</h2>`)
	if strings.Contains(plain, "<svg") || strings.Contains(plain, "<p") {
		t.Fatalf("EmptyState without icon or body = %q", plain)
	}
}

func TestKeyValueRendersPairsAndACopyButton(t *testing.T) {
	got := renderKit(t, KeyValue(web.LayoutData{}, KeyValueProps{Items: []KeyValueItem{
		{Label: "Wallet", Value: "Cash"},
		{Label: "ID", Value: "abc-123", Mono: true, Copy: true},
		{Label: "Empty", Copy: true},
	}}), "")
	requireContainsAll(t, got,
		`<dl class="divide-y divide-base-300">`,
		`<dt`, ">Wallet</dt>", ">Cash</span>",
		`class="font-mono text-xs break-all"`,
		`data-copy="abc-123"`,
		`aria-label="common.copy: ID"`,
	)
	if n := strings.Count(got, "data-copy="); n != 1 {
		t.Fatalf("want one copy button (an empty value gets none), got %d: %q", n, got)
	}
}

func TestKeyValueDenseIsATwoColumnListAndANodeReplacesTheValue(t *testing.T) {
	got := renderKit(t, KeyValue(web.LayoutData{}, KeyValueProps{Dense: true, Items: []KeyValueItem{
		{Label: "Date", Value: "ignored", Node: templ.Raw(`<time>today</time>`)},
	}}), "")
	requireContainsAll(t, got, `<dl class="grid gap-y-2 text-sm">`, `<div class="grid grid-cols-3 gap-x-3">`, `<time>today</time>`)
	if strings.Contains(got, "ignored") || strings.Contains(got, "divide-y") {
		t.Fatalf("dense KeyValue = %q", got)
	}
}
