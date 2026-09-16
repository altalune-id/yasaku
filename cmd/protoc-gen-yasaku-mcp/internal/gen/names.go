package gen

import (
	"regexp"
	"strings"
	"unicode"

	"google.golang.org/protobuf/compiler/protogen"
)

var toolNamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,63}$`)

func snakeCase(s string) string {
	runes := []rune(s)
	var b strings.Builder
	b.Grow(len(runes) + 4)
	for i, r := range runes {
		if !unicode.IsUpper(r) {
			b.WriteRune(r)
			continue
		}
		if i > 0 && (!unicode.IsUpper(runes[i-1]) || (i+1 < len(runes) && unicode.IsLower(runes[i+1]))) {
			b.WriteByte('_')
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

func comment(c protogen.Comments) string {
	var parts []string
	for line := range strings.SplitSeq(string(c), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			parts = append(parts, line)
		}
	}
	return strings.Join(parts, " ")
}
