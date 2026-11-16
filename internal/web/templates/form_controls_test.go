package templates

import (
	"regexp"
	"strings"
	"testing"
)

var (
	formControl  = regexp.MustCompile(`(?s)<(input|select|textarea)\b(.*?)>`)
	controlType  = regexp.MustCompile(`\btype="([^"]+)"`)
	controlClass = regexp.MustCompile(`(?s)class=("[^"]*"|\{[^}]*\})`)
	stringConst  = regexp.MustCompile(`(?m)^\s*(\w+)\s*=\s*"([^"]*)"`)
)

// TestFormControlsUseDaisyUIClasses keeps every select, text-like input and textarea on daisyUI's own control class, which owns padding and the caret.
func TestFormControlsUseDaisyUIClasses(t *testing.T) {
	root := repoRoot(t)
	files := templFiles(t, root)
	consts := map[string]string{}
	srcs := map[string]string{}
	for _, f := range files {
		srcs[f] = readSource(t, root, f)
		for _, m := range stringConst.FindAllStringSubmatch(srcs[f], -1) {
			consts[m[1]] = m[2]
		}
	}
	exempt := map[string]bool{"hidden": true, "checkbox": true, "radio": true, "file": true, "submit": true, "button": true, "reset": true, "range": true, "color": true, "image": true}
	checked := 0
	for _, f := range files {
		for _, m := range formControl.FindAllStringSubmatch(srcs[f], -1) {
			tag, attrs := m[1], m[2]
			if typ := controlType.FindStringSubmatch(attrs); tag == "input" && typ != nil && exempt[typ[1]] {
				continue
			}
			checked++
			cls := ""
			if c := controlClass.FindStringSubmatch(attrs); c != nil {
				cls = c[1]
			}
			for name, val := range consts {
				cls = regexp.MustCompile(`\b`+name+`\b`).ReplaceAllString(cls, `"`+val+`"`)
			}
			if !regexp.MustCompile(`["\s]` + tag + `["\s]`).MatchString(cls) {
				t.Errorf("%s: <%s> lacks the daisyUI %q class: %s", f, tag, tag, oneLine(strings.TrimSpace(attrs)))
			}
		}
	}
	if checked < 40 {
		t.Fatalf("checked only %d controls; the scan is broken", checked)
	}
}
