//go:build dev

package auth

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// Only compiled with `-tags dev`, for local testing without Google. Release
// builds (Docker, CI) don't contain this route at all.
func init() {
	devMount = func(r chi.Router, a *Auth) {
		r.Get("/auth/dev-login", func(w http.ResponseWriter, r *http.Request) {
			email := strings.ToLower(r.URL.Query().Get("email"))
			if !a.allowed[email] {
				http.Error(w, "email is not on MIMIR_ALLOWED_EMAILS", http.StatusForbidden)
				return
			}
			if err := a.startSession(w, r, email, "dev"); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			http.Redirect(w, r, "/", http.StatusFound)
		})
	}
}
