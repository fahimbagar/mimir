package library

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
)

type epubMeta struct {
	title     string
	author    string
	coverPath string // path inside the zip
	coverType string
}

type epubContainer struct {
	Rootfiles []struct {
		FullPath  string `xml:"full-path,attr"`
		MediaType string `xml:"media-type,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opfItem struct {
	ID         string `xml:"id,attr"`
	Href       string `xml:"href,attr"`
	MediaType  string `xml:"media-type,attr"`
	Properties string `xml:"properties,attr"`
}

type opfPackage struct {
	Metadata struct {
		Titles   []string `xml:"title"`
		Creators []string `xml:"creator"`
		Metas    []struct {
			Name    string `xml:"name,attr"`
			Content string `xml:"content,attr"`
		} `xml:"meta"`
	} `xml:"metadata"`
	Manifest []opfItem `xml:"manifest>item"`
}

// Book covers are rarely bigger than a few MB; this bounds memory if a file
// lies about its size.
const maxZipEntry = 32 << 20

func readEPUBMeta(file string) (epubMeta, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return epubMeta{}, err
	}
	defer func() { _ = zr.Close() }() // read-only; nothing to flush

	raw, err := readEntry(&zr.Reader, "META-INF/container.xml")
	if err != nil {
		return epubMeta{}, fmt.Errorf("container.xml: %w", err)
	}
	var c epubContainer
	if err := xml.Unmarshal(raw, &c); err != nil {
		return epubMeta{}, fmt.Errorf("container.xml: %w", err)
	}
	opfPath := ""
	for _, rf := range c.Rootfiles {
		if rf.MediaType == "" || rf.MediaType == "application/oebps-package+xml" {
			opfPath = rf.FullPath
			break
		}
	}
	if opfPath == "" {
		return epubMeta{}, errors.New("no OPF rootfile")
	}

	raw, err = readEntry(&zr.Reader, opfPath)
	if err != nil {
		return epubMeta{}, fmt.Errorf("opf: %w", err)
	}
	var pkg opfPackage
	if err := xml.Unmarshal(raw, &pkg); err != nil {
		return epubMeta{}, fmt.Errorf("opf: %w", err)
	}

	var m epubMeta
	for _, t := range pkg.Metadata.Titles {
		if t = strings.TrimSpace(t); t != "" {
			m.title = t
			break
		}
	}
	var authors []string
	for _, a := range pkg.Metadata.Creators {
		if a = strings.TrimSpace(a); a != "" {
			authors = append(authors, a)
		}
	}
	m.author = strings.Join(authors, ", ")

	if item, ok := findCover(pkg); ok {
		href, err := url.PathUnescape(item.Href)
		if err != nil {
			href = item.Href
		}
		m.coverPath = path.Join(path.Dir(opfPath), href)
		m.coverType = item.MediaType
	}
	return m, nil
}

func findCover(pkg opfPackage) (opfItem, bool) {
	isImage := func(it opfItem) bool { return strings.HasPrefix(it.MediaType, "image/") }

	// EPUB 3
	for _, it := range pkg.Manifest {
		if isImage(it) && containsWord(it.Properties, "cover-image") {
			return it, true
		}
	}
	// EPUB 2: <meta name="cover" content="item-id"/>
	for _, meta := range pkg.Metadata.Metas {
		if meta.Name != "cover" {
			continue
		}
		for _, it := range pkg.Manifest {
			if it.ID == meta.Content && isImage(it) {
				return it, true
			}
		}
	}
	// Last resort: an image whose id or file name says "cover".
	for _, it := range pkg.Manifest {
		if isImage(it) && (strings.Contains(strings.ToLower(it.ID), "cover") ||
			strings.Contains(strings.ToLower(path.Base(it.Href)), "cover")) {
			return it, true
		}
	}
	return opfItem{}, false
}

func containsWord(list, word string) bool {
	for _, w := range strings.Fields(list) {
		if w == word {
			return true
		}
	}
	return false
}

func readZipEntry(file, name string) ([]byte, error) {
	zr, err := zip.OpenReader(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	return readEntry(&zr.Reader, name)
}

func readEntry(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		if f.UncompressedSize64 > maxZipEntry {
			return nil, fmt.Errorf("%s: entry too large", name)
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(io.LimitReader(rc, maxZipEntry))
	}
	return nil, ErrNotFound
}
