package server

import (
	"archive/zip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"

	"mimir/internal/auth"
	"mimir/internal/library"
	"mimir/internal/store"
)

const pdfBody = "%PDF-1.4 0123456789"

const testUser = "reader@example.com"

// newApp wires a server over lib with a real store and auth, and returns it
// with a Cookie header value for a signed-in, allowlisted user.
func newApp(t *testing.T, lib *library.Library, static string) (*httptest.Server, string) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a, err := auth.New(context.Background(), auth.Config{
		BaseURL:       "https://mimir.example.com",
		ClientID:      "id",
		ClientSecret:  "secret",
		AllowedEmails: []string{testUser},
		// Never contacted: these tests don't go through Google.
		Provider: &oidc.ProviderConfig{IssuerURL: "https://idp.invalid", JWKSURL: "https://idp.invalid/jwks"},
	}, st)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.UpsertUser(ctx, testUser, ""); err != nil {
		t.Fatal(err)
	}
	token, err := st.CreateSession(ctx, testUser, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	h, err := New(lib, st, a, static, "https://mimir.example.com").Routes()
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, auth.SessionCookie + "=" + token
}

type testServer struct {
	*httptest.Server
	books   []library.Book
	session map[string]string // headers that carry the session cookie
}

func newTestServer(t *testing.T, withStatic bool) testServer {
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
	ts, cookie := newApp(t, lib, static)
	return testServer{ts, list, map[string]string{"Cookie": cookie}}
}

type response struct {
	status int
	header http.Header
	body   string
}

func get(t *testing.T, url string, header map[string]string) response {
	t.Helper()
	return send(t, http.MethodGet, url, header, "")
}

func send(t *testing.T, method, url string, header map[string]string, body string) response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rd)
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
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response{res.StatusCode, res.Header, string(raw)}
}

func TestListBooks(t *testing.T) {
	ts := newTestServer(t, true)
	res := get(t, ts.URL+"/api/books", ts.session)
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
	ts := newTestServer(t, true)
	id := ts.books[0].Files[0].ID

	res := get(t, ts.URL+"/api/files/"+id, ts.session)
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
	res = get(t, ts.URL+"/api/files/"+id, map[string]string{"Range": "bytes=0-7", "Cookie": ts.session["Cookie"]})
	if res.status != http.StatusPartialContent || res.body != "%PDF-1.4" {
		t.Errorf("range read: %d %q", res.status, res.body)
	}
}

func TestNotFound(t *testing.T) {
	ts := newTestServer(t, true)
	pdfID := ts.books[0].Files[0].ID
	for _, path := range []string{
		"/api/files/unknown",
		"/api/files/" + pdfID + "/cover", // PDFs have no embedded cover
		"/api/files/..%2f..%2fetc%2fpasswd",
		"/api/nope", // unknown API routes must not fall through to the SPA
	} {
		res := get(t, ts.URL+path, ts.session)
		if res.status != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", path, res.status)
		}
		if strings.Contains(res.body, "<!doctype html>") {
			t.Errorf("%s: served index.html", path)
		}
	}
}

func TestSPA(t *testing.T) {
	ts := newTestServer(t, true)

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
	ts := newTestServer(t, false)
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
	ts, cookie := newApp(t, lib, t.TempDir())

	res := get(t, ts.URL+"/api/files/"+books[0].Files[0].ID+"/cover", map[string]string{"Cookie": cookie})
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

func TestAPIRequiresSignIn(t *testing.T) {
	ts := newTestServer(t, true)
	id := ts.books[0].Files[0].ID
	for _, path := range []string{"/api/me", "/api/books", "/api/files/" + id, "/api/files/" + id + "/cover"} {
		for name, header := range map[string]map[string]string{
			"no cookie":     nil,
			"forged cookie": {"Cookie": auth.SessionCookie + "=forged"},
		} {
			res := get(t, ts.URL+path, header)
			if res.status != http.StatusUnauthorized {
				t.Errorf("%s (%s): status %d, want 401", path, name, res.status)
			}
			if strings.Contains(res.body, pdfBody) {
				t.Errorf("%s (%s): leaked the book", path, name)
			}
		}
	}
}

func TestMe(t *testing.T) {
	ts := newTestServer(t, true)
	res := get(t, ts.URL+"/api/me", ts.session)
	if res.status != http.StatusOK || !strings.Contains(res.body, testUser) {
		t.Errorf("got %d %q", res.status, res.body)
	}
}

func TestCrossSiteWritesRejected(t *testing.T) {
	ts := newTestServer(t, true)
	header := map[string]string{
		"Cookie":         ts.session["Cookie"],
		"Origin":         "https://evil.example",
		"Sec-Fetch-Site": "cross-site",
	}
	if res := send(t, http.MethodPost, ts.URL+"/auth/logout", header, ""); res.status != http.StatusForbidden {
		t.Errorf("cross-site logout: status %d, want 403", res.status)
	}
	// Same-origin requests from the app itself still work.
	header["Origin"], header["Sec-Fetch-Site"] = "https://mimir.example.com", "same-origin"
	if res := send(t, http.MethodPost, ts.URL+"/auth/logout", header, ""); res.status != http.StatusNoContent {
		t.Errorf("same-origin logout: status %d, want 204", res.status)
	}
}

func TestProgress(t *testing.T) {
	ts := newTestServer(t, true)
	id := ts.books[0].Files[0].ID
	write := map[string]string{"Cookie": ts.session["Cookie"], "Content-Type": "application/json", "Sec-Fetch-Site": "same-origin"}

	res := get(t, ts.URL+"/api/progress", ts.session)
	if res.status != http.StatusOK || strings.TrimSpace(res.body) != "[]" {
		t.Fatalf("empty list: %d %q", res.status, res.body)
	}

	if res := send(t, http.MethodPut, ts.URL+"/api/progress/"+id, write, `{"position": 12, "progress": 0.25}`); res.status != http.StatusNoContent {
		t.Fatalf("save: %d %q", res.status, res.body)
	}
	if res := send(t, http.MethodPut, ts.URL+"/api/progress/"+id, write, `{"position": "epubcfi(/6/4!/4/2)", "progress": 0.5}`); res.status != http.StatusNoContent {
		t.Fatalf("save string: %d %q", res.status, res.body)
	}

	res = get(t, ts.URL+"/api/progress", ts.session)
	var list []struct {
		FileID   string          `json:"fileId"`
		Position json.RawMessage `json:"position"`
		Progress float64         `json:"progress"`
	}
	if err := json.Unmarshal([]byte(res.body), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].FileID != id || string(list[0].Position) != `"epubcfi(/6/4!/4/2)"` || list[0].Progress != 0.5 {
		t.Errorf("unexpected list: %s", res.body)
	}
}

func TestProgressRejectsBadInput(t *testing.T) {
	ts := newTestServer(t, true)
	id := ts.books[0].Files[0].ID
	write := map[string]string{"Cookie": ts.session["Cookie"], "Sec-Fetch-Site": "same-origin"}
	for name, body := range map[string]string{
		"not json":       `{`,
		"no position":    `{"progress": 0.5}`,
		"zero page":      `{"position": 0, "progress": 0.5}`,
		"fractional":     `{"position": 1.5, "progress": 0.5}`,
		"object":         `{"position": {"a": 1}, "progress": 0.5}`,
		"empty string":   `{"position": "", "progress": 0.5}`,
		"progress > 1":   `{"position": 3, "progress": 1.5}`,
		"negative":       `{"position": 3, "progress": -0.1}`,
		"unknown field":  `{"position": 3, "progress": 0.1, "email": "other@example.com"}`,
		"huge position":  `{"position": "` + strings.Repeat("x", 5000) + `", "progress": 0.1}`,
		"oversized body": `{"position": 3, "progress": 0.1, "pad": "` + strings.Repeat("x", 9000) + `"}`,
	} {
		if res := send(t, http.MethodPut, ts.URL+"/api/progress/"+id, write, body); res.status != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", name, res.status)
		}
	}
	if res := send(t, http.MethodPut, ts.URL+"/api/progress/unknown", write, `{"position": 3, "progress": 0.1}`); res.status != http.StatusNotFound {
		t.Errorf("unknown file: status %d, want 404", res.status)
	}
	if res := send(t, http.MethodPut, ts.URL+"/api/progress/"+id, map[string]string{"Sec-Fetch-Site": "same-origin"}, `{"position": 3, "progress": 0.1}`); res.status != http.StatusUnauthorized {
		t.Errorf("no session: status %d, want 401", res.status)
	}
	cross := map[string]string{"Cookie": ts.session["Cookie"], "Origin": "https://evil.example", "Sec-Fetch-Site": "cross-site"}
	if res := send(t, http.MethodPut, ts.URL+"/api/progress/"+id, cross, `{"position": 3, "progress": 0.1}`); res.status != http.StatusForbidden {
		t.Errorf("cross-site: status %d, want 403", res.status)
	}
}
