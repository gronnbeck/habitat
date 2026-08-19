package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gronnbeck/habitat/internal/store"
)

const sessionCookie = "habitat_session"

type contextKey struct{}

// guard requires a signed-in user when authentication is enabled. With it
// disabled — the local, loopback-only case — it is a pass-through, so running
// habitat on your own machine needs no account.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.cfg.RequireAuth {
			next(w, r)
			return
		}
		user, err := s.currentUser(r)
		if err != nil {
			http.Redirect(w, r, "/login?next="+r.URL.Path, http.StatusSeeOther)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, user)))
	}
}

func (s *Server) currentUser(r *http.Request) (store.User, error) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.User{}, errors.New("no session")
	}
	return s.store.UserBySession(cookie.Value)
}

func userFrom(ctx context.Context) *store.User {
	user, ok := ctx.Value(contextKey{}).(store.User)
	if !ok {
		return nil
	}
	return &user
}

func (s *Server) handleLoginForm(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.RequireAuth {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	s.render(w, r, "login.html", map[string]any{"Next": r.URL.Query().Get("next")})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	user, err := s.store.Authenticate(r.PostFormValue("email"), r.PostFormValue("password"))
	if err != nil {
		// One message for both an unknown address and a wrong password: the
		// form must not become a way to discover who has an account.
		w.WriteHeader(http.StatusUnauthorized)
		s.render(w, r, "login.html", map[string]any{
			"Error": "That email and password don't match.",
			"Next":  r.PostFormValue("next"),
		})
		return
	}
	token, err := s.store.CreateSession(user.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.SecureCookies,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(store.SessionLifetime),
	})
	next := r.PostFormValue("next")
	if next == "" || !isLocalPath(next) {
		next = "/"
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		_ = s.store.DeleteSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true,
		Secure: s.cfg.SecureCookies, MaxAge: -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// isLocalPath keeps the post-login redirect on this site. "//evil.example" is
// a valid URL to a different host, so checking for a leading slash alone is
// not enough.
func isLocalPath(path string) bool {
	return len(path) > 0 && path[0] == '/' && (len(path) == 1 || path[1] != '/')
}
