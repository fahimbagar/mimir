// Package server exposes the library as a JSON API and serves the web app.
package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/netip"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"mimir/internal/auth"
	"mimir/internal/library"
	"mimir/internal/store"
)

// Server serves the library API and the built frontend.
type Server struct {
	lib       *library.Library
	store     *store.Store
	auth      *auth.Auth
	staticDir string
	baseURL   string
	proxies   []netip.Prefix
}

// New returns a server for lib. staticDir holds the built frontend and
// baseURL is the public origin (see auth.ParseBaseURL).
func New(lib *library.Library, st *store.Store, a *auth.Auth, staticDir, baseURL string) *Server {
	return &Server{lib: lib, store: st, auth: a, staticDir: staticDir, baseURL: baseURL}
}

// TrustProxies lets requests from these addresses name the visitor in
// X-Real-IP, for the request log only. The proxy must overwrite that header,
// never pass on one sent by the client.
func (s *Server) TrustProxies(nets []netip.Prefix) *Server {
	s.proxies = nets
	return s
}

// Routes returns the HTTP handler.
func (s *Server) Routes() (http.Handler, error) {
	r := chi.NewRouter()
	// No RealIP middleware: forwarded headers are client-controlled unless a
	// trusted proxy sets them, and would let anyone spoof log entries. The
	// request log reads X-Real-IP itself, from TrustProxies addresses only.
	r.Use(requestLog(s.proxies), middleware.Recoverer, securityHeaders(strings.HasPrefix(s.baseURL, "https://")))

	s.auth.Mount(r)
	// Everything under /api, including the book files themselves, needs a
	// signed-in, allowlisted user. The SPA shell stays public so the login
	// page can load; it holds no data.
	r.Route("/api", func(r chi.Router) {
		r.Use(s.auth.RequireUser)
		r.Get("/me", s.me)
		r.Get("/books", s.listBooks)
		r.Get("/progress", s.listProgress)
		r.Put("/progress/{id}", s.saveProgress)
		r.Get("/files/{id}", s.serveFile)
		r.Get("/files/{id}/cover", s.serveCover)
	})
	r.Handle("/*", s.spa())

	// Rejects cross-site POST/PUT/DELETE (CSRF) using Sec-Fetch-Site and
	// Origin. The public origin is trusted explicitly because a reverse
	// proxy may rewrite the Host header.
	cop := http.NewCrossOriginProtection()
	if err := cop.AddTrustedOrigin(s.baseURL); err != nil {
		return nil, err
	}
	return cop.Handler(r), nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	writeJSON(w, u)
}

type progressJSON struct {
	FileID    string          `json:"fileId"`
	Position  json.RawMessage `json:"position"`
	Progress  float64         `json:"progress"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

func (s *Server) listProgress(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	list, err := s.store.ListProgress(r.Context(), u.Email, 100)
	if err != nil {
		slog.Error("list progress", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]progressJSON, len(list))
	for i, p := range list {
		out[i] = progressJSON{p.FileID, json.RawMessage(p.Position), p.Progress, p.UpdatedAt}
	}
	writeJSON(w, out)
}

// A CFI is rarely over a few hundred characters.
const maxPositionLen = 2048

func (s *Server) saveProgress(w http.ResponseWriter, r *http.Request) {
	u, _ := auth.UserFrom(r.Context())
	id := chi.URLParam(r, "id")
	if _, err := s.lib.File(id); err != nil {
		s.notFoundOr500(w, err)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)
	var body struct {
		Position json.RawMessage `json:"position"`
		Progress float64         `json:"progress"`
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	pos, ok := canonicalPosition(body.Position)
	if !ok || math.IsNaN(body.Progress) || body.Progress < 0 || body.Progress > 1 {
		http.Error(w, "position must be a page number or a short string, progress 0..1", http.StatusBadRequest)
		return
	}
	err := s.store.SaveProgress(r.Context(), u.Email, store.Progress{FileID: id, Position: pos, Progress: body.Progress})
	if err != nil {
		slog.Error("save progress", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// canonicalPosition accepts a page number (PDF) or a non-empty string (EPUB
// CFI) and re-encodes it, so only clean JSON reaches the database.
func canonicalPosition(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || len(raw) > maxPositionLen {
		return "", false
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", false
	}
	switch p := v.(type) {
	case float64:
		if p < 1 || p != math.Trunc(p) || p > 1e7 {
			return "", false
		}
	case string:
		if p == "" {
			return "", false
		}
	default:
		return "", false
	}
	out, err := json.Marshal(v)
	return string(out), err == nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("write json", "err", err)
	}
}

func (s *Server) listBooks(w http.ResponseWriter, _ *http.Request) {
	books, err := s.lib.Books()
	if err != nil {
		slog.Error("scan library", "err", err)
		http.Error(w, "failed to scan library", http.StatusInternalServerError)
		return
	}
	writeJSON(w, books)
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
	// Keep book bytes out of the browser's disk cache.
	w.Header().Set("Cache-Control", "private, no-store")
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
