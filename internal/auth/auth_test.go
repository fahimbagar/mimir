package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"github.com/go-jose/go-jose/v4"

	"mimir/internal/store"
)

const clientID = "test-client"

// fakeIDP is a minimal OpenID provider: a token endpoint that checks PKCE and
// returns an RS256-signed ID token, plus its JWKS.
type fakeIDP struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	claims map[string]any // ID token claims; nonce is filled from the auth request

	challenge string // code_challenge from the authorize redirect
	nonce     string
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIDP{key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		if r.PostForm.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != idp.challenge {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		claims := map[string]any{
			"iss": idp.srv.URL, "aud": clientID, "sub": "123",
			"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
			"nonce": idp.nonce,
		}
		for k, v := range idp.claims {
			claims[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 3600,
			"id_token": idp.sign(t, claims),
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *fakeIDP) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	obj, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	s, err := obj.CompactSerialize()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type env struct {
	idp   *fakeIDP
	store *store.Store
	app   *httptest.Server
}

func newEnv(t *testing.T) *env {
	t.Helper()
	idp := newFakeIDP(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	a, err := New(context.Background(), Config{
		BaseURL:       "http://localhost:8080",
		ClientID:      clientID,
		ClientSecret:  "secret",
		AllowedEmails: []string{"Reader@Example.com"},
		Provider: &oidc.ProviderConfig{
			IssuerURL:  idp.srv.URL,
			AuthURL:    idp.srv.URL + "/authorize",
			TokenURL:   idp.srv.URL + "/token",
			JWKSURL:    idp.srv.URL + "/jwks",
			Algorithms: []string{oidc.RS256},
		},
	}, st)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	a.Mount(r)
	r.With(a.RequireUser).Get("/private", func(w http.ResponseWriter, r *http.Request) {
		u, _ := UserFrom(r.Context())
		_, _ = w.Write([]byte(u.Email))
	})
	app := httptest.NewServer(r)
	t.Cleanup(app.Close)
	return &env{idp: idp, store: st, app: app}
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// result is a response with the body already closed.
type result struct {
	status  int
	header  http.Header
	cookies []*http.Cookie
}

func cookieFrom(res result, name string) *http.Cookie {
	for _, c := range res.cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func do(t *testing.T, method, url string, cookies ...*http.Cookie) result {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	return result{res.StatusCode, res.Header, res.Cookies()}
}

// signIn runs /auth/login then /auth/callback and returns the callback
// response. mutate can tamper with the callback query.
func (e *env) signIn(t *testing.T, mutate func(q url.Values)) result {
	t.Helper()
	res := do(t, http.MethodGet, e.app.URL+"/auth/login")
	if res.status != http.StatusFound {
		t.Fatalf("login status %d", res.status)
	}
	flow := cookieFrom(res, flowCookie)
	if flow == nil || !flow.HttpOnly || !flow.Secure || flow.SameSite != http.SameSiteLaxMode {
		t.Fatalf("flow cookie not locked down: %+v", flow)
	}
	authURL, err := url.Parse(res.header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	aq := authURL.Query()
	if aq.Get("code_challenge_method") != "S256" || aq.Get("redirect_uri") != "http://localhost:8080/auth/callback" {
		t.Fatalf("authorize request missing PKCE or redirect: %s", authURL)
	}
	e.idp.challenge, e.idp.nonce = aq.Get("code_challenge"), aq.Get("nonce")

	q := url.Values{"code": {"good-code"}, "state": {aq.Get("state")}}
	if mutate != nil {
		mutate(q)
	}
	return do(t, http.MethodGet, e.app.URL+"/auth/callback?"+q.Encode(), flow)
}

func TestSignInAllowedUser(t *testing.T) {
	e := newEnv(t)
	e.idp.claims = map[string]any{"email": "reader@example.com", "email_verified": true, "name": "Reader"}

	res := e.signIn(t, nil)
	if res.status != http.StatusFound || res.header.Get("Location") != "/" {
		t.Fatalf("callback: %d -> %s", res.status, res.header.Get("Location"))
	}
	session := cookieFrom(res, SessionCookie)
	if session == nil || session.Value == "" || !session.HttpOnly || !session.Secure || session.Path != "/" {
		t.Fatalf("session cookie: %+v", session)
	}

	if res := do(t, http.MethodGet, e.app.URL+"/private", session); res.status != http.StatusOK {
		t.Fatalf("private with session: %d", res.status)
	}

	res = do(t, http.MethodPost, e.app.URL+"/auth/logout", session)
	if res.status != http.StatusNoContent {
		t.Fatalf("logout: %d", res.status)
	}
	if res := do(t, http.MethodGet, e.app.URL+"/private", session); res.status != http.StatusUnauthorized {
		t.Errorf("session still valid after logout: %d", res.status)
	}
}

func TestSignInRejected(t *testing.T) {
	cases := []struct {
		name   string
		claims map[string]any
		mutate func(url.Values)
		want   string
	}{
		{"not on allowlist", map[string]any{"email": "stranger@example.com", "email_verified": true}, nil, "denied"},
		{"unverified email", map[string]any{"email": "reader@example.com", "email_verified": false}, nil, "denied"},
		{"forged state", map[string]any{"email": "reader@example.com", "email_verified": true},
			func(q url.Values) { q.Set("state", "attacker") }, "failed"},
		{"wrong nonce", map[string]any{"email": "reader@example.com", "email_verified": true, "nonce": "replayed"}, nil, "failed"},
		{"wrong audience", map[string]any{"email": "reader@example.com", "email_verified": true, "aud": "other-app"}, nil, "failed"},
		{"bad code", map[string]any{"email": "reader@example.com", "email_verified": true},
			func(q url.Values) { q.Set("code", "stolen") }, "failed"},
		{"user cancelled", nil, func(q url.Values) { q.Set("error", "access_denied") }, "cancelled"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.idp.claims = c.claims
			res := e.signIn(t, c.mutate)
			if loc := res.header.Get("Location"); loc != "/login?error="+c.want {
				t.Errorf("redirect %q, want /login?error=%s", loc, c.want)
			}
			if s := cookieFrom(res, SessionCookie); s != nil && s.Value != "" {
				t.Error("got a session cookie")
			}
		})
	}
}

func TestCallbackWithoutFlowCookie(t *testing.T) {
	e := newEnv(t)
	res := do(t, http.MethodGet, e.app.URL+"/auth/callback?code=good-code&state=x")
	if loc := res.header.Get("Location"); loc != "/login?error=expired" {
		t.Errorf("redirect %q", loc)
	}
}

func TestRemovedFromAllowlist(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	// A session that predates the allowlist change.
	if err := e.store.UpsertUser(ctx, "former@example.com", ""); err != nil {
		t.Fatal(err)
	}
	token, err := e.store.CreateSession(ctx, "former@example.com", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	res := do(t, http.MethodGet, e.app.URL+"/private", &http.Cookie{Name: SessionCookie, Value: token})
	if res.status != http.StatusUnauthorized {
		t.Errorf("status %d, want 401", res.status)
	}
}

func TestParseBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.dev":   "https://example.dev",
		"https://example.dev/":  "https://example.dev",
		"http://localhost:8080": "http://localhost:8080",
		"http://127.0.0.1:8080": "http://127.0.0.1:8080",
	} {
		if got, err := ParseBaseURL(in); err != nil || got != want {
			t.Errorf("ParseBaseURL(%q) = %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "example.dev", "http://example.dev", "https://x.y/path", "ftp://x.y"} {
		if _, err := ParseBaseURL(bad); err == nil {
			t.Errorf("ParseBaseURL(%q) should fail", bad)
		}
	}
}

func TestNewRejectsEmptyAllowlist(t *testing.T) {
	_, err := New(context.Background(), Config{BaseURL: "http://localhost", ClientID: "a", ClientSecret: "b", AllowedEmails: []string{" , "}}, nil)
	if err == nil || !strings.Contains(err.Error(), "MIMIR_ALLOWED_EMAILS") {
		t.Errorf("err = %v", err)
	}
}
