package blog_test

import (
	"strings"
	"testing"

	"altalune.id/yasaku/internal/blog"
)

func TestRenderHTML_NeutralisesHostileInput(t *testing.T) {
	tests := []struct {
		name, in, mustNotContain string
	}{
		{"raw script tag", "<script>alert(1)</script>", "<script>"},
		{"inline event handler", `<img src=x onerror=alert(1)>`, "onerror"},
		{"javascript link", "[x](javascript:alert(1))", "javascript:"},
		{"javascript image", "![x](javascript:alert(1))", "javascript:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := blog.RenderHTML(tt.in)
			if strings.Contains(got, tt.mustNotContain) {
				t.Fatalf("RenderHTML(%q) = %q, must not contain %q", tt.in, got, tt.mustNotContain)
			}
		})
	}
}

func TestRenderHTML_RendersOrdinaryMarkdown(t *testing.T) {
	got := blog.RenderHTML("## Title\n\nSome **bold** text.")
	for _, want := range []string{"<h2", "<strong>bold</strong>"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderHTML output %q missing %q", got, want)
		}
	}
}

func TestRenderHTML_EmptyInput(t *testing.T) {
	if got := blog.RenderHTML(""); strings.TrimSpace(got) != "" {
		t.Errorf("RenderHTML(\"\") = %q, want empty", got)
	}
}
