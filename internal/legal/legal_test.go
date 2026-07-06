package legal

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"THIRD_PARTY_NOTICES.md":  {Data: []byte("# Third-Party Notices\n\n## example/lib\n- License: MIT\n")},
		"licenses/MIT.txt":        {Data: []byte("MIT License text")},
		"licenses/Apache-2.0.txt": {Data: []byte("Apache License text")},
	}
}

func TestIndexPage(t *testing.T) {
	h := Handler(testFS())
	req := httptest.NewRequest(http.MethodGet, "/legal", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content-type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Third-Party Notices") {
		t.Errorf("body missing notices heading")
	}
	// License files are listed as links.
	if !strings.Contains(body, `href="licenses/MIT.txt"`) {
		t.Errorf("body missing MIT license link:\n%s", body)
	}
	if !strings.Contains(body, `href="licenses/Apache-2.0.txt"`) {
		t.Errorf("body missing Apache license link")
	}
}

func TestRawNotices(t *testing.T) {
	h := Handler(testFS())
	req := httptest.NewRequest(http.MethodGet, "/legal/THIRD_PARTY_NOTICES.md", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("content-type = %q, want text/markdown", ct)
	}
	if !strings.Contains(rec.Body.String(), "# Third-Party Notices") {
		t.Errorf("raw markdown not served")
	}
}

func TestRawLicenseFile(t *testing.T) {
	h := Handler(testFS())
	req := httptest.NewRequest(http.MethodGet, "/legal/licenses/MIT.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "MIT License text") {
		t.Errorf("license file not served, got %q", body)
	}
}

func TestUnknownLicenseFile404(t *testing.T) {
	h := Handler(testFS())
	req := httptest.NewRequest(http.MethodGet, "/legal/licenses/does-not-exist.txt", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
