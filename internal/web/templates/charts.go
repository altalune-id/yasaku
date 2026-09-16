package templates

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/a-h/templ"
)

// jsonScriptEscaper keeps a payload from closing its own <script> element; the sequences stay valid JSON.
var jsonScriptEscaper = strings.NewReplacer( //nolint:gochecknoglobals // immutable replacer
	"<", `\u003c`,
	">", `\u003e`,
	"&", `\u0026`,
)

// ChartJSON marshals v into the payload string Chart embeds; it returns "null" when v cannot be encoded.
func ChartJSON(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(v); err != nil {
		return "null"
	}
	return string(bytes.TrimRight(buf.Bytes(), "\n"))
}

func chartPayloadScript(id, payload string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<script type="application/json" data-chart-for="`+
			templ.EscapeString(id)+`">`+jsonScriptEscaper.Replace(payload)+`</script>`)
		return err
	})
}
