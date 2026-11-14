package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"slices"
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

// ParseProxies parses a comma-separated list of IP addresses and CIDR
// prefixes, such as "172.20.0.1, 10.0.0.0/8".
func ParseProxies(list string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for f := range strings.SplitSeq(list, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if p, err := netip.ParsePrefix(f); err == nil {
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is not an IP address or CIDR prefix", f)
		}
		out = append(out, netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()))
	}
	return out, nil
}

// clientAddr returns the visitor address a trusted proxy put in X-Real-IP,
// or "" when the peer isn't one of trusted or sent no valid address. Anyone
// can send X-Real-IP, so it counts only when it comes from our own proxy.
func clientAddr(r *http.Request, trusted []netip.Prefix) string {
	if len(trusted) == 0 {
		return ""
	}
	peer, err := netip.ParseAddrPort(r.RemoteAddr)
	if err != nil {
		return ""
	}
	ip := peer.Addr().Unmap()
	if !slices.ContainsFunc(trusted, func(p netip.Prefix) bool { return p.Contains(ip) }) {
		return ""
	}
	c, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("X-Real-IP")))
	if err != nil {
		return ""
	}
	return c.Unmap().String()
}

// requestLog logs one line per request. It logs the path only: the query of
// /auth/callback carries the OAuth code and state, which don't belong in logs.
// Requests through a trusted proxy also get the visitor's address as client.
func requestLog(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			defer func() {
				status := ww.Status()
				if status == 0 {
					status = http.StatusOK
				}
				attrs := []any{"method", r.Method, "path", r.URL.Path, "status", status,
					"bytes", ww.BytesWritten(), "dur", time.Since(start).Round(time.Millisecond), "remote", r.RemoteAddr}
				if c := clientAddr(r, trusted); c != "" {
					attrs = append(attrs, "client", c)
				}
				slog.Info("http", attrs...)
			}()
			next.ServeHTTP(ww, r)
		})
	}
}
