package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"altalune.id/yasaku/internal/i18n"
	"altalune.id/yasaku/internal/web"
)

func TestSeatsLine_PluralForms(t *testing.T) {
	en := web.LayoutData{Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.EnUS)}
	assert.Equal(t, "1 of 1 seat (0 pending)", seatsLine(en, 1, "Used", "1", "Max", "1", "Pending", "0"))
	assert.Equal(t, "2 of 5 seats (1 pending)", seatsLine(en, 5, "Used", "2", "Max", "5", "Pending", "1"))
	ar := web.LayoutData{Translator: i18n.NewEmbeddedBundle(i18n.EnUS).For(i18n.ArSA)}
	assert.NotEqual(t, "limits.seats", seatsLine(ar, 1, "Used", "1", "Max", "1", "Pending", "0"), "every locale has the plural forms")
}
