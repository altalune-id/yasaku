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

var dialogCloseButton = regexp.MustCompile(`<button[^>]*data-dialog-close[^>]*>`)

func TestDialogRendersANativeDialogWithLabel(t *testing.T) {
	child := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<p id="dialog-child">hello</p>`)
		return err
	})
	var sb strings.Builder
	ctx := templ.WithChildren(context.Background(), child)
	if err := Dialog(web.LayoutData{Nonce: "n0nce"}, "demo", "Demo title").Render(ctx, &sb); err != nil {
		t.Fatalf("render: %v", err)
	}
	got := sb.String()

	for _, want := range []string{
		`<dialog id="demo" class="dialog `,
		`aria-labelledby="demo-title"`,
		`id="demo-title"`,
		"Demo title",
		`<p id="dialog-child">hello</p>`,
		`src="/static/dialog.js`,
		`nonce="n0nce"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("Dialog() = %q, want it to contain %q", got, want)
		}
	}
	btn := dialogCloseButton.FindString(got)
	if btn == "" {
		t.Fatalf("Dialog() = %q, want a [data-dialog-close] button", got)
	}
	if !strings.Contains(btn, `aria-label="`) {
		t.Fatalf("close button %q has no aria-label", btn)
	}
	if strings.Contains(got, "onclick") {
		t.Fatalf("Dialog() must carry no inline handler: %q", got)
	}
}
