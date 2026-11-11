package library

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseFilename(t *testing.T) {
	cases := []struct{ in, title, author string }{
		{
			"Technical Analysis Using Multiple Timeframes (Brian Shannon) (z-library.sk, 1lib.sk, z-lib.sk)",
			"Technical Analysis Using Multiple Timeframes", "Brian Shannon",
		},
		{
			"The master swing trader  tools and techniques (Farley, Alan S) (z-library.sk, 1lib.sk, z-lib.sk)",
			"The master swing trader tools and techniques", "Alan S Farley",
		},
		{"Market Wizards - Interview with Top American Traders", "Market Wizards - Interview with Top American Traders", ""},
		{"One Up On Wall Street_ How To Use It", "One Up On Wall Street: How To Use It", ""},
		{"Annual Report (2000)", "Annual Report (2000)", ""},
	}
	for _, c := range cases {
		title, author := parseFilename(c.in)
		if title != c.title || author != c.author {
			t.Errorf("parseFilename(%q) = %q, %q; want %q, %q", c.in, title, author, c.title, c.author)
		}
	}
}

func writeEPUB(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
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
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

func TestBooksGroupsFormatsAndReadsEPUBMeta(t *testing.T) {
	dir := t.TempDir()
	writeEPUB(t, filepath.Join(dir, "some file.epub"), map[string]string{
		"META-INF/container.xml": containerXML,
		"OEBPS/content.opf": `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Real Title</dc:title>
    <dc:creator>Jane Doe</dc:creator>
    <meta name="cover" content="img1"/>
  </metadata>
  <manifest><item id="img1" href="images/front%20page.jpg" media-type="image/jpeg"/></manifest>
</package>`,
		"OEBPS/images/front page.jpg": "JPEGDATA",
	})
	if err := os.WriteFile(filepath.Join(dir, "some file.pdf"), []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	lib, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	books, err := lib.Books()
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("got %d books, want 1: %+v", len(books), books)
	}
	b := books[0]
	if b.Title != "Real Title" || b.Author != "Jane Doe" {
		t.Errorf("got %q by %q", b.Title, b.Author)
	}
	if len(b.Files) != 2 || b.Files[0].Format != EPUB || b.Files[1].Format != PDF {
		t.Fatalf("unexpected files: %+v", b.Files)
	}
	if !b.Files[0].HasCover {
		t.Error("epub cover not detected")
	}

	data, typ, err := lib.Cover(b.Files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "JPEGDATA" || typ != "image/jpeg" {
		t.Errorf("cover = %q (%s)", data, typ)
	}
	if _, _, err := lib.Cover(b.Files[1].ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("pdf cover err = %v, want ErrNotFound", err)
	}
	if _, err := lib.File("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id err = %v, want ErrNotFound", err)
	}
}
