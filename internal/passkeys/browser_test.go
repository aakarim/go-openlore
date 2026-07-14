package passkeys

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderFileIncludesBreadcrumbsIframeAndParentCloseLink(t *testing.T) {
	pk := &Passkeys{}
	req := httptest.NewRequest("GET", "/lore/guides/setup.md", nil)
	rec := httptest.NewRecorder()

	pk.renderFile(rec, req, "/lore", "/guides/setup.md")

	body := rec.Body.String()
	for _, want := range []string{
		`href="/lore/"`,
		`href="/lore/guides/"`,
		`aria-current="page">setup.md`,
		`href="/lore/guides" aria-label="Close file and return to folder"`,
		`src="/lore/guides/setup.md?raw=1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("file view missing %q", want)
		}
	}
}

func TestRenderMarkdownFormatsGFMAndDoesNotRenderRawHTML(t *testing.T) {
	rec := httptest.NewRecorder()
	source := []byte("---\ntype: guide\ntags:\n  - setup\n  - web\n---\n# Guide\n\n| A | B |\n| - | - |\n| one | two |\n\n<script>alert('no')</script>\n")

	if err := renderMarkdown(rec, source); err != nil {
		t.Fatalf("renderMarkdown: %v", err)
	}

	body := rec.Body.String()
	for _, want := range []string{
		"<h1>Guide</h1>",
		"<table>",
		`<base target="_top">`,
		`<section class="frontmatter"`,
		"type: guide\ntags:\n  - setup\n  - web",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rendered markdown missing %q", want)
		}
	}
	if strings.Contains(body, "<hr>") {
		t.Fatal("frontmatter delimiters must not be rendered as Markdown")
	}
	if strings.Contains(body, "<script>") {
		t.Fatal("raw HTML must not be rendered into the markdown document")
	}
	if got := rec.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
}
