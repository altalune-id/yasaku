package handlers_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

var (
	pageTitleKey = regexp.MustCompile(`(?:pageTitle|walletLayout|h\.get)\([^,]+,\s*"([^"]+)"|writeWalletForm\(.*,\s*"([^"]+)"\)`)
	literalTitle = regexp.MustCompile(`\b(Base|Layout|LayoutForOrg|LayoutForProject|walletLayout|layout|writeWalletForm)\((?:[^()]|\([^()]*\))*?"[A-Z][^"]*"`)
	bareTitle    = regexp.MustCompile(`\b(?:Base|Layout|LayoutForOrg|LayoutForProject)\([^,]+, [\w.]+\.Name,`)
	errorTitle   = regexp.MustCompile(`ErrorPage\([^,]+, [^,]+, [^,]+, "[^"]*"`)
	joinedTitle  = regexp.MustCompile(`"[^"]*" · "|" · "\s*\+|\+\s*" · "`)
)

const localeFileGlob = "../../i18n/locales/active.*.yaml"

func TestPageTitles_ComeFromFilledLocaleKeys(t *testing.T) {
	locales, err := filepath.Glob(localeFileGlob)
	if err != nil || len(locales) < 5 {
		t.Fatalf("locales: %v %v", locales, err)
	}
	msgs := map[string]map[string]any{}
	for _, f := range locales {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]any{}
		if err := yaml.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		msgs[filepath.Base(f)] = m
	}
	sources, _ := filepath.Glob("*.go")
	keys := 0
	for _, f := range sources {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		for _, m := range literalTitle.FindAllString(src, -1) {
			t.Errorf("%s: page title is an English literal: %s", f, m)
		}
		for _, m := range bareTitle.FindAllString(src, -1) {
			t.Errorf("%s: page title is a bare name with no page label: %s", f, m)
		}
		for _, m := range errorTitle.FindAllString(src, -1) {
			t.Errorf("%s: error page title is an English literal: %s", f, m)
		}
		if joinedTitle.MatchString(src) && f != "handlers.go" {
			t.Errorf("%s: joins a title by hand; use pageTitle", f)
		}
		for _, m := range pageTitleKey.FindAllStringSubmatch(src, -1) {
			keys++
			key := m[1] + m[2]
			for name, loc := range msgs {
				if s, _ := loc[key].(string); s == "" {
					t.Errorf("%s: title key %s is missing or empty in %s", f, key, name)
				}
			}
		}
	}
	if keys == 0 {
		t.Fatal("no pageTitle call found; the scan matches nothing")
	}
}
