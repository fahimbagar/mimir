// Package library scans a directory of ebooks and groups files that share a
// name (e.g. "Foo.pdf" and "Foo.epub") into a single book.
package library

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// Format is a supported ebook file type.
type Format string

// Supported formats, named after their file extensions.
const (
	PDF  Format = "pdf"
	EPUB Format = "epub"
)

// ErrNotFound is returned for unknown file IDs and missing covers.
var ErrNotFound = errors.New("not found")

// File is one ebook file on disk. ID is derived from its path relative to
// the library root and stays stable across scans.
type File struct {
	ID       string    `json:"id"`
	Format   Format    `json:"format"`
	Size     int64     `json:"size"`
	Name     string    `json:"name"`
	HasCover bool      `json:"hasCover"`
	ModTime  time.Time `json:"modTime"`

	path string
}

// Path is the absolute path on disk.
func (f File) Path() string { return f.path }

// Book groups the files that share a name, e.g. "Foo.pdf" and "Foo.epub".
type Book struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Author  string    `json:"author,omitempty"`
	Files   []File    `json:"files"`
	AddedAt time.Time `json:"addedAt"`
}

// Library indexes the ebooks under one directory.
type Library struct {
	root string

	mu    sync.Mutex
	files map[string]File          // by file ID, from the last scan
	meta  map[string]epubMetaEntry // epub metadata cache, by path
}

type epubMetaEntry struct {
	size    int64
	modTime time.Time
	meta    epubMeta
	err     error
}

// New opens the library at root, which must be an existing directory.
func New(root string) (*Library, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}
	return &Library{root: abs, files: map[string]File{}, meta: map[string]epubMetaEntry{}}, nil
}

// Root is the absolute path of the library directory.
func (l *Library) Root() string { return l.root }

// Books rescans the directory. Scanning is cheap; EPUB metadata is cached
// per file and only re-read when size or mtime changes.
func (l *Library) Books() ([]Book, error) {
	type entry struct {
		rel string
		fi  fs.FileInfo
		fmt Format
	}
	var entries []entry
	err := filepath.WalkDir(l.root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") && p != l.root {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		var f Format
		switch strings.ToLower(filepath.Ext(name)) {
		case ".pdf":
			f = PDF
		case ".epub":
			f = EPUB
		default:
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(l.root, p)
		if err != nil {
			return err
		}
		entries = append(entries, entry{rel: rel, fi: fi, fmt: f})
		return nil
	})
	if err != nil {
		return nil, err
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	files := make(map[string]File, len(entries))
	groups := map[string]*Book{}
	var order []string
	for _, e := range entries {
		abs := filepath.Join(l.root, e.rel)
		file := File{
			ID:      hashID(e.rel),
			Format:  e.fmt,
			Size:    e.fi.Size(),
			Name:    filepath.Base(e.rel),
			ModTime: e.fi.ModTime(),
			path:    abs,
		}

		title, author := parseFilename(strings.TrimSuffix(file.Name, filepath.Ext(file.Name)))
		fromMeta := false
		if e.fmt == EPUB {
			if m, err := l.epubMetaLocked(abs, e.fi); err == nil {
				file.HasCover = m.coverPath != ""
				if m.title != "" {
					title, fromMeta = m.title, true
				}
				if m.author != "" {
					author = m.author
				}
			}
		}
		files[file.ID] = file

		key := strings.ToLower(strings.TrimSuffix(e.rel, filepath.Ext(e.rel)))
		b, ok := groups[key]
		if !ok {
			b = &Book{ID: hashID(key), Title: title, Author: author}
			groups[key] = b
			order = append(order, key)
		} else if fromMeta {
			// Embedded metadata beats whatever we guessed from a filename.
			b.Title = title
			if author != "" {
				b.Author = author
			}
		} else if b.Author == "" {
			b.Author = author
		}
		b.Files = append(b.Files, file)
		if file.ModTime.After(b.AddedAt) {
			b.AddedAt = file.ModTime
		}
	}
	l.files = files

	books := make([]Book, 0, len(order))
	for _, k := range order {
		b := groups[k]
		sort.Slice(b.Files, func(i, j int) bool { return b.Files[i].Format < b.Files[j].Format })
		books = append(books, *b)
	}
	sort.SliceStable(books, func(i, j int) bool {
		return strings.ToLower(books[i].Title) < strings.ToLower(books[j].Title)
	})
	return books, nil
}

// File looks up a file by ID. IDs come only from scans, so a client can never
// make us open a path it chose.
func (l *Library) File(id string) (File, error) {
	l.mu.Lock()
	f, ok := l.files[id]
	l.mu.Unlock()
	if ok {
		return f, nil
	}
	// Files added since the last scan.
	if _, err := l.Books(); err != nil {
		return File{}, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if f, ok := l.files[id]; ok {
		return f, nil
	}
	return File{}, ErrNotFound
}

// Cover returns the embedded cover image of an EPUB.
func (l *Library) Cover(id string) ([]byte, string, error) {
	f, err := l.File(id)
	if err != nil {
		return nil, "", err
	}
	if f.Format != EPUB {
		return nil, "", ErrNotFound
	}
	fi, err := os.Stat(f.path)
	if err != nil {
		return nil, "", err
	}
	l.mu.Lock()
	m, err := l.epubMetaLocked(f.path, fi)
	l.mu.Unlock()
	if err != nil {
		return nil, "", err
	}
	if m.coverPath == "" {
		return nil, "", ErrNotFound
	}
	data, err := readZipEntry(f.path, m.coverPath)
	if err != nil {
		return nil, "", err
	}
	return data, m.coverType, nil
}

func (l *Library) epubMetaLocked(path string, fi fs.FileInfo) (epubMeta, error) {
	if c, ok := l.meta[path]; ok && c.size == fi.Size() && c.modTime.Equal(fi.ModTime()) {
		return c.meta, c.err
	}
	m, err := readEPUBMeta(path)
	l.meta[path] = epubMetaEntry{size: fi.Size(), modTime: fi.ModTime(), meta: m, err: err}
	return m, err
}

func hashID(s string) string {
	sum := sha256.Sum256([]byte(filepath.ToSlash(s)))
	return hex.EncodeToString(sum[:10])
}

var (
	// "(z-library.sk, 1lib.sk, z-lib.sk)" and similar download-site tags.
	siteTagRe    = regexp.MustCompile(`(?i)\s*\([^)]*\b(z-?lib|1lib|libgen|annas?-archive)[^)]*\)\s*$`)
	trailParenRe = regexp.MustCompile(`^(.*\S)\s*\(([^()]+)\)$`)
	yearRe       = regexp.MustCompile(`^\d{4}$`)
	spacesRe     = regexp.MustCompile(`\s+`)
)

// parseFilename guesses title and author from a file stem. Only the
// unambiguous "Title (Author)" shape yields an author; "A - B" is left alone
// because both orders are common.
func parseFilename(stem string) (title, author string) {
	s := siteTagRe.ReplaceAllString(stem, "")
	// Many tools write ':' as '_' since it is not allowed in filenames.
	s = strings.ReplaceAll(s, "_ ", ": ")
	s = strings.ReplaceAll(s, "_", " ")
	s = strings.TrimSpace(spacesRe.ReplaceAllString(s, " "))

	if m := trailParenRe.FindStringSubmatch(s); m != nil && !yearRe.MatchString(strings.TrimSpace(m[2])) {
		s, author = m[1], strings.TrimSpace(m[2])
		// "Farley, Alan S" -> "Alan S Farley"
		if parts := strings.Split(author, ","); len(parts) == 2 {
			author = strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[0])
		}
	}
	return s, author
}
