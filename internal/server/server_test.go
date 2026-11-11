package server

import (
	"archive/zip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mimir/internal/library"
)

const pdfBody = "%PDF-1.4 0123456789"

func newTestServer(t *testing.T, withStatic bool) (*httptest.Server, []library.Book) {
	t.Helper()
	books := t.TempDir()
	if err := os.WriteFile(filepath.Join(books, "Some Book (Jane Doe).pdf"), []byte(pdfBody), 0o644); err != nil {
		t.Fatal(err)
	}

	static := t.TempDir()
	if withStatic {
		if err := os.WriteFile(filepath.Join(static, "index.html"), []byte("<!doctype html>app"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(static, "assets"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(static, "assets", "app.js"), []byte("console.log(1)"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	lib, err := library.New(books)
	if err != nil {
		t.Fatal(err)
	}
	list, err := lib.Books()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(lib, static).Routes())
	t.Cleanup(ts.Close)
	return ts, list
}

type response struct {
	status int
	header http.Header
	body   string
}

func get(t *testing.T, url string, header map[string]string) response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{res.StatusCode, res.Header, string(body)}
}

func TestListBooks(t *testing.T) {
	ts, _ := newTestServer(t, true)
	res := get(t, ts.URL+"/api/books", nil)
	if res.status != http.StatusOK {
		t.Fatalf("status %d", res.status)
	}
	if ct := res.header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content type %q", ct)
	}
	var books []library.Book
	if err := json.Unmarshal([]byte(res.body), &books); err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 || books[0].Title != "Some Book" || books[0].Author != "Jane Doe" {
		t.Errorf("unexpected books: %+v", books)
	}
}

func TestServeFile(t *testing.T) {
	ts, books := newTestServer(t, true)
	id := books[0].Files[0].ID

	res := get(t, ts.URL+"/api/files/"+id, nil)
	if res.status != http.StatusOK || res.body != pdfBody {
		t.Fatalf("full read: %d %q", res.status, res.body)
	}
	if ct := res.header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("content type %q", ct)
	}
	if cd := res.header.Get("Content-Disposition"); !strings.HasPrefix(cd, "inline;") || !strings.Contains(cd, "Some Book") {
		t.Errorf("content disposition %q", cd)
	}

	// pdf.js depends on byte ranges for large files.
	res = get(t, ts.URL+"/api/files/"+id, map[string]string{"Range": "bytes=0-7"})
	if res.status != http.StatusPartialContent || res.body != "%PDF-1.4" {
		t.Errorf("range read: %d %q", res.status, res.body)
	}
}

func TestNotFound(t *testing.T) {
	ts, books := newTestServer(t, true)
	pdfID := books[0].Files[0].ID
	for _, path := range []string{
		"/api/files/unknown",
		"/api/files/" + pdfID + "/cover", // PDFs have no embedded cover
		"/api/files/..%2f..%2fetc%2fpasswd",
		"/api/nope", // unknown API routes must not fall through to the SPA
	} {
		res := get(t, ts.URL+path, nil)
		if res.status != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, res.status)
		}
		if strings.Contains(res.body, "<!doctype html>") {
			t.Errorf("%s: served index.html", path)
		}
	}
}

func TestSPA(t *testing.T) {
	ts, _ := newTestServer(t, true)

	res := get(t, ts.URL+"/assets/app.js", nil)
	if res.status != http.StatusOK || res.body != "console.log(1)" {
		t.Errorf("asset: %d %q", res.status, res.body)
	}

	for _, path := range []string{"/", "/read/abc123", "/assets"} {
		res := get(t, ts.URL+path, nil)
		if res.status != http.StatusOK || res.body != "<!doctype html>app" {
			t.Errorf("%s: %d %q, want index.html", path, res.status, res.body)
		}
	}

	res = get(t, ts.URL+"/../../etc/passwd", nil)
	if strings.Contains(res.body, "root:") {
		t.Errorf("path traversal served %q", res.body)
	}
}

func TestSPAWithoutBuild(t *testing.T) {
	ts, _ := newTestServer(t, false)
	res := get(t, ts.URL+"/", nil)
	if res.status != http.StatusNotFound || !strings.Contains(res.body, "frontend not built") {
		t.Errorf("got %d %q", res.status, res.body)
	}
}

func TestCoverIsSandboxed(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "evil.epub"))
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"META-INF/container.xml": `<container><rootfiles><rootfile full-path="content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`,
		"content.opf": `<package><metadata><title>Evil</title></metadata>
			<manifest><item id="c" href="cover.svg" media-type="image/svg+xml" properties="cover-image"/></manifest></package>`,
		"cover.svg": `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	lib, err := library.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	books, err := lib.Books()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(lib, t.TempDir()).Routes())
	t.Cleanup(ts.Close)

	res := get(t, ts.URL+"/api/files/"+books[0].Files[0].ID+"/cover", nil)
	if res.status != http.StatusOK || res.header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("got %d %s", res.status, res.header.Get("Content-Type"))
	}
	if csp := res.header.Get("Content-Security-Policy"); !strings.Contains(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("CSP %q does not sandbox the cover", csp)
	}
	if res.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing nosniff")
	}
}
