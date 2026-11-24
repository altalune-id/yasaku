package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func TestErrorBanner_ShortRefFullTitleAndCopyTarget(t *testing.T) {
	const rid = "01a20ae3-040e-7795-9868-f3c9a42f9d80"
	d := web.LayoutData{Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS), RequestID: rid}

	body := render(t, ErrorBanner(d, "Acme has 2 seats, all taken.", "LIM001"))

	assert.Contains(t, body, `role="alert"`)
	assert.Contains(t, body, "Acme has 2 seats, all taken.")
	assert.Contains(t, body, "LIM001")
	assert.Contains(t, body, "01a20ae3…", "the footer shows the first 8 characters")
	assert.Contains(t, body, `title="Reference: `+rid+`"`, "the full reference stays in the title")
	assert.Contains(t, body, `data-copy="`+rid+`"`, "Copy copies the full reference")
	assert.Contains(t, body, `aria-label="Copy reference"`)
}

func TestErrorBanner_LeavesOutEmptyParts(t *testing.T) {
	tr := i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)

	bare := render(t, ErrorBanner(web.LayoutData{Translator: tr}, "Name is required.", ""))
	assert.NotContains(t, bare, "data-error-footer", "no footer without a code or a reference")
	assert.NotContains(t, bare, "data-copy")

	codeOnly := render(t, ErrorBanner(web.LayoutData{Translator: tr}, "Name is required.", "VAL001"))
	assert.Contains(t, codeOnly, "data-error-footer")
	assert.Contains(t, codeOnly, "VAL001")
	assert.NotContains(t, codeOnly, "data-copy", "no Copy without a reference")
}
