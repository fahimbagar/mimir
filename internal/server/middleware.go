package server

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

// contentPolicy is the CSP for the app. Scripts only come from our own
// origin; inline <style> is allowed because epub.js injects the book's CSS
// into its iframes, and blob: covers the book resources and pdf.js fonts.
var contentPolicy = strings.Join([]string{
	"default-src 'self'",
	"script-src 'self'",
	"style-src 'self' 'unsafe-inline'",
	"img-src 'self' data: blob:",
	"font-src 'self' data: blob:",
	"connect-src 'self'",
	"worker-src 'self' blob:",
	"frame-src 'self' blob:",
	"object-src 'none'",
	"base-uri 'none'",
	"form-action 'self'",
	"frame-ancestors 'none'",
}, "; ")

// securityHeaders sets browser protections on every response. Handlers may
// override them (the cover endpoint uses a stricter CSP).
func securityHeaders(https bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("Content-Security-Policy", contentPolicy)
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			// Other sites can't pull our files into <img>/<script>/fetch.
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			if https {
				h.Set("Strict-Transport-Security", "max-age=63072000")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requestLog logs one line per request. It logs the path only: the query of
// /auth/callback carries the OAuth code and state, which don't belong in logs.
func requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			slog.Info("http", "method", r.Method, "path", r.URL.Path, "status", status,
				"bytes", ww.BytesWritten(), "dur", time.Since(start).Round(time.Millisecond), "remote", r.RemoteAddr)
		}()
		next.ServeHTTP(ww, r)
	})
}
