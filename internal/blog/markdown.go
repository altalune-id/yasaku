package blog

import (
	"bytes"

	"github.com/yuin/goldmark"
)

// SECURITY: no html.WithUnsafe here — post bodies are user-authored, unlike internal/legal's trusted content.
var md = goldmark.New() //nolint:gochecknoglobals // goldmark.Markdown is a stateless, concurrency-safe fixture, not runtime state.

// RenderHTML converts post markdown to HTML, escaping any embedded raw HTML.
func RenderHTML(src string) string {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	return buf.String()
}
