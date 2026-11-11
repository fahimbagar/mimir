// Package server exposes the library as a JSON API and serves the web app.
package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"mimir/internal/library"
)

// Server serves the library API and the built frontend.
type Server struct {
	lib       *library.Library
	staticDir string
}

// New returns a server for lib. staticDir holds the built frontend.
func New(lib *library.Library, staticDir string) *Server {
	return &Server{lib: lib, staticDir: staticDir}
}

// Routes returns the HTTP handler.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	// No RealIP middleware: without a trusted proxy in front, forwarded
	// headers are client-controlled and would let anyone spoof log entries.
	r.Use(middleware.Logger, middleware.Recoverer)

	r.Route("/api", func(r chi.Router) {
		r.Get("/books", s.listBooks)
		r.Get("/files/{id}", s.serveFile)
		r.Get("/files/{id}/cover", s.serveCover)
	})
	r.Handle("/*", s.spa())
	return r
}

func (s *Server) listBooks(w http.ResponseWriter, _ *http.Request) {
	books, err := s.lib.Books()
	if err != nil {
		slog.Error("scan library", "err", err)
		http.Error(w, "failed to scan library", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(books); err != nil {
		slog.Warn("write books", "err", err)
	}
}

func (s *Server) serveFile(w http.ResponseWriter, r *http.Request) {
	f, err := s.lib.File(chi.URLParam(r, "id"))
	if err != nil {
		s.notFoundOr500(w, err)
		return
	}
	fh, err := os.Open(f.Path()) //nolint:gosec // path comes from our own scan, looked up by ID; never from the request
	if err != nil {
		s.notFoundOr500(w, err)
		return
	}
	defer func() { _ = fh.Close() }()
	st, err := fh.Stat()
	if err != nil {
		s.notFoundOr500(w, err)
		return
	}

	ct := "application/pdf"
	if f.Format == library.EPUB {
		ct = "application/epub+zip"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": f.Name}))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	// ServeContent handles Range requests, which pdf.js uses to load large
	// PDFs piece by piece.
	http.ServeContent(w, r, f.Name, st.ModTime(), fh)
}

func (s *Server) serveCover(w http.ResponseWriter, r *http.Request) {
	data, ct, err := s.lib.Cover(chi.URLParam(r, "id"))
	if err != nil {
		s.notFoundOr500(w, err)
		return
	}
	if ct == "" {
		ct = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	// Covers come from untrusted EPUBs and may be SVG, which can carry
	// scripts. <img> never runs them, but opening the URL directly would, on
	// our origin. nosniff + a sandboxing CSP makes that inert.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	_, _ = w.Write(data) //nolint:gosec // see the CSP above
}

func (s *Server) notFoundOr500(w http.ResponseWriter, err error) {
	if errors.Is(err, library.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	slog.Error("serve", "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// spa serves the built frontend and falls back to index.html so client-side
// routes like /read/<id> survive a reload.
func (s *Server) spa() http.Handler {
	root := http.Dir(s.staticDir)
	files := http.FileServer(root)
	index := filepath.Join(s.staticDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f, err := root.Open(path.Clean("/" + r.URL.Path)); err == nil {
			st, err := f.Stat()
			_ = f.Close()
			if err == nil && !st.IsDir() {
				files.ServeHTTP(w, r)
				return
			}
		}
		if _, err := os.Stat(index); err != nil {
			http.Error(w, "frontend not built: run `make web` (or use the Vite dev server)", http.StatusNotFound)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}
