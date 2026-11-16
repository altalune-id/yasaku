package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestStaticThemesCSS_FollowsTheWritingDirection(t *testing.T) {
	b, err := staticFS.ReadFile("static/themes.css")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if hit := regexp.MustCompile(`text-align:\s*(left|right)`).FindString(src); hit != "" {
		t.Errorf("themes.css uses %q; use start or end", hit)
	}
	if strings.Contains(src, "translateX(") && !strings.Contains(src, ":dir(rtl)") {
		t.Error("themes.css nudges along x with no :dir(rtl) mirror")
	}
}
