// Package auth signs users in with Google and keeps them signed in with a
// session cookie. Only emails on the allowlist get a session.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"

	"mimir/internal/store"
)

const (
	// SessionCookie holds the session token. The __Host- prefix makes the
	// browser refuse it unless it is Secure, host-only and Path=/, so a
	// sibling subdomain can't plant or overwrite it.
	SessionCookie = "__Host-mimir_session"
	flowCookie    = "__Host-mimir_oauth"
	flowTTL       = 10 * time.Minute
)

// Google's endpoints, written out so startup needs no discovery request.
var googleProvider = oidc.ProviderConfig{ //nolint:gosec // public endpoint URLs, not credentials
	IssuerURL:   "https://accounts.google.com",
	AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:    "https://oauth2.googleapis.com/token",
	UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
	JWKSURL:     "https://www.googleapis.com/oauth2/v3/certs",
	Algorithms:  []string{oidc.RS256},
}

// Config configures sign-in.
type Config struct {
	// BaseURL is the public origin, e.g. https://example.dev. The
	// OAuth redirect is BaseURL + "/auth/callback".
	BaseURL       string
	ClientID      string
	ClientSecret  string
	AllowedEmails []string
	SessionTTL    time.Duration
	// Provider overrides Google; used by tests.
	Provider *oidc.ProviderConfig
}

// User is the signed-in user.
type User struct {
	Email string `json:"email"`
}

// Auth handles sign-in, sign-out and session checks.
type Auth struct {
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
	allowed  map[string]bool
	store    *store.Store
	ttl      time.Duration
}

// New validates cfg and prepares the OAuth client.
func New(ctx context.Context, cfg Config, st *store.Store) (*Auth, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are required")
	}
	allowed := ParseAllowlist(strings.Join(cfg.AllowedEmails, ","))
	if len(allowed) == 0 {
		// Fail closed: an empty list must not mean "everyone".
		return nil, errors.New("MIMIR_ALLOWED_EMAILS must list at least one email")
	}
	base, err := ParseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	ttl := cfg.SessionTTL
	if ttl <= 0 {
		ttl = 30 * 24 * time.Hour
	}
	pc := cfg.Provider
	if pc == nil {
		pc = &googleProvider
	}
	provider := pc.NewProvider(ctx)
	return &Auth{
		oauth: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			Endpoint:     provider.Endpoint(),
			RedirectURL:  base + "/auth/callback",
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		allowed:  allowed,
		store:    st,
		ttl:      ttl,
	}, nil
}

// ParseBaseURL checks that the public URL is an https origin (plain http is
// only accepted for localhost) and returns it without a trailing slash.
func ParseBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return "", fmt.Errorf("MIMIR_BASE_URL %q must be an origin like https://mimir.example.com", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if host := u.Hostname(); host != "localhost" && !isLoopback(host) {
			return "", fmt.Errorf("MIMIR_BASE_URL %q: plain http is only allowed for localhost", raw)
		}
	default:
		return "", fmt.Errorf("MIMIR_BASE_URL %q must use https", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ParseAllowlist splits a comma- or space-separated list of emails.
func ParseAllowlist(list string) map[string]bool {
	out := map[string]bool{}
	for _, e := range strings.FieldsFunc(list, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		if e = strings.ToLower(strings.TrimSpace(e)); e != "" {
			out[e] = true
		}
	}
	return out
}

var devMount func(r chi.Router, a *Auth) // set by devlogin.go in dev builds only

// Mount registers the /auth routes.
func (a *Auth) Mount(r chi.Router) {
	r.Get("/auth/login", a.login)
	r.Get("/auth/callback", a.callback)
	r.Post("/auth/logout", a.logout)
	if devMount != nil {
		slog.Warn("DEV BUILD: /auth/dev-login signs in any allowlisted email without Google")
		devMount(r, a)
	}
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	state, nonce := randomString(), randomString()
	verifier := oauth2.GenerateVerifier()
	setCookie(w, flowCookie, state+"."+nonce+"."+verifier, flowTTL)
	target := a.oauth.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "select_account"),
	)
	http.Redirect(w, r, target, http.StatusFound)
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	fail := func(reason string, err error) {
		slog.Warn("sign-in failed", "reason", reason, "err", err)
		http.Redirect(w, r, "/login?error="+reason, http.StatusFound)
	}
	clearCookie(w, flowCookie)

	q := r.URL.Query()
	if q.Get("error") != "" {
		fail("cancelled", errors.New(q.Get("error")))
		return
	}
	c, err := r.Cookie(flowCookie)
	if err != nil {
		fail("expired", err)
		return
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 3 || subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1 {
		fail("failed", errors.New("state mismatch"))
		return
	}
	nonce, verifier := parts[1], parts[2]

	tok, err := a.oauth.Exchange(r.Context(), q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		fail("failed", err)
		return
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		fail("failed", errors.New("no id_token in token response"))
		return
	}
	idToken, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		fail("failed", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(nonce)) != 1 {
		fail("failed", errors.New("nonce mismatch"))
		return
	}
	var claims struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		fail("failed", err)
		return
	}
	email := strings.ToLower(claims.Email)
	if !claims.EmailVerified || !a.allowed[email] {
		fail("denied", fmt.Errorf("%q is not allowed (verified=%t)", email, claims.EmailVerified))
		return
	}
	if err := a.startSession(w, r, email, claims.Name); err != nil {
		fail("failed", err)
		return
	}
	slog.Info("signed in", "email", email)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *Auth) startSession(w http.ResponseWriter, r *http.Request, email, name string) error {
	if err := a.store.UpsertUser(r.Context(), email, name); err != nil {
		return err
	}
	token, err := a.store.CreateSession(r.Context(), email, a.ttl)
	if err != nil {
		return err
	}
	setCookie(w, SessionCookie, token, a.ttl)
	return nil
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		if err := a.store.DeleteSession(r.Context(), c.Value); err != nil {
			slog.Error("delete session", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	clearCookie(w, SessionCookie)
	w.WriteHeader(http.StatusNoContent)
}

type ctxKey struct{}

// RequireUser rejects requests without a live session for an allowed email.
// The allowlist is checked on every request, so removing someone and
// restarting locks them out even with an old cookie.
func (a *Auth) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(SessionCookie)
		if err != nil {
			unauthorized(w)
			return
		}
		email, err := a.store.SessionEmail(r.Context(), c.Value)
		if errors.Is(err, store.ErrNotFound) || (err == nil && !a.allowed[email]) {
			unauthorized(w)
			return
		}
		if err != nil {
			slog.Error("session lookup", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, User{Email: email})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// UserFrom returns the user set by RequireUser.
func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(ctxKey{}).(User)
	return u, ok
}

func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"sign in required"}`))
}

func setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   true, // browsers also accept Secure cookies on http://localhost
		// Lax, not Strict: the cookie must survive the redirect back from Google.
		SameSite: http.SameSiteLaxMode,
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
}

func randomString() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b) // crypto/rand.Read never fails on supported platforms
	return base64.RawURLEncoding.EncodeToString(b)
}
