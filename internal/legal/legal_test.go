package legal_test

import (
	"strconv"
	"strings"
	"testing"

	"altalune.id/yasaku/internal/legal"
)

func TestTerms_ParsesAndRenders(t *testing.T) {
	t.Parallel()
	d, err := legal.Terms()
	if err != nil {
		t.Fatalf("Terms() error: %v", err)
	}
	if d.Slug != legal.TermsSlug {
		t.Errorf("slug = %q, want %q", d.Slug, legal.TermsSlug)
	}
	if d.Title == "" {
		t.Error("title empty — frontmatter not parsed")
	}
	if d.UpdatedAt.IsZero() {
		t.Error("updatedAt zero — frontmatter date not parsed")
	}
	if !strings.Contains(d.HTML, "<h2>") || strings.Contains(d.HTML, "<h1>") {
		t.Error("HTML needs section <h2>s and no <h1>: the page header shows the title")
	}
}

func TestPrivacy_ParsesAndRenders(t *testing.T) {
	t.Parallel()
	d, err := legal.Privacy()
	if err != nil {
		t.Fatalf("Privacy() error: %v", err)
	}
	if d.Title == "" {
		t.Error("title empty")
	}
	if !strings.Contains(d.HTML, "<h2>") || strings.Contains(d.HTML, "<h1>") {
		t.Error("HTML needs section <h2>s and no <h1>: the page header shows the title")
	}
}

func TestBySlug(t *testing.T) {
	t.Parallel()
	tests := []struct {
		slug    string
		wantErr bool
	}{
		{legal.TermsSlug, false},
		{legal.PrivacySlug, false},
		{"unknown", true},
	}
	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			t.Parallel()
			_, err := legal.BySlug(tc.slug)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestPrivacy_NoTemplateText(t *testing.T) {
	t.Parallel()
	d, err := legal.Privacy()
	if err != nil {
		t.Fatalf("Privacy() error: %v", err)
	}
	body := strings.ToLower(d.WithContact("").HTML)
	for _, gone := range []string{"todo", "sample", "placeholder", "%contact%"} {
		if strings.Contains(body, gone) {
			t.Errorf("privacy still carries template text %q", gone)
		}
	}
	for _, want := range []string{
		strconv.Itoa(legal.BackupDays) + " days",
		"1 year",
		"read-only",
		"reason",
		"emailed",
		"activity",
		"within 72 hours and finish within 30 days",
		"never sell",
		"opensheet",
		"ai assistant",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("privacy is missing %q", want)
		}
	}
}

func TestTerms_FairUse(t *testing.T) {
	t.Parallel()
	d, err := legal.Terms()
	if err != nil {
		t.Fatalf("Terms() error: %v", err)
	}
	if !strings.Contains(d.HTML, "Fair-use limits apply.") {
		t.Error("terms are missing the fair-use sentence")
	}
}

func TestPrivacy_ContactFillsTheEmailSentences(t *testing.T) {
	t.Parallel()
	d, err := legal.Privacy()
	if err != nil {
		t.Fatalf("Privacy() error: %v", err)
	}
	with := d.WithContact("help@example.com").HTML
	if got := strings.Count(with, `<a class="link" href="mailto:help@example.com">help@example.com</a>`); got != 2 {
		t.Errorf("contact link count = %d, want 2 (account deletion and questions)", got)
	}
	if !strings.Contains(with, "To delete your account, email <a class=\"link\" href=") {
		t.Error("the deletion sentence does not name the address")
	}
	without := d.WithContact("  ").HTML
	if strings.Contains(without, "mailto:") || strings.Contains(without, "%CONTACT%") {
		t.Error("an empty contact leaves a link or the raw token")
	}
	if !strings.Contains(with, `<h2>8. Contact</h2>
<p>Send privacy questions to <a class="link" href="mailto:help@example.com">help@example.com</a>.</p>`) {
		t.Error("the Contact section is missing or the questions line is not its paragraph")
	}
	for _, gone := range []string{"To delete your account", "Send privacy questions", "email the operator", "Contact</h2>"} {
		if strings.Contains(without, gone) {
			t.Errorf("an empty contact keeps %q", gone)
		}
	}
	if !strings.Contains(without, "We confirm an account deletion within 72 hours and finish within 30 days.") {
		t.Error("the deletion promise goes with the contact sentence")
	}
	if !strings.Contains(d.HTML, "%CONTACT%") {
		t.Error("WithContact changed the shared document")
	}
	escaped := d.WithContact(`a"<b>@example.com`).HTML
	if strings.Contains(escaped, "<b>") {
		t.Error("the contact is not escaped")
	}
}
