// Package auth implements Google sign-in, sessions and the session gate.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/sameera/geronimo/internal/store"
	"github.com/sameera/geronimo/internal/web"
)

const (
	// Host-only secure-prefix cookie names (D4). The prefix needs Secure and Path=/.
	sessionCookie = "__Host-geronimo_session"
	attemptCookie = "__Host-geronimo_attempt"

	attemptTTL = 10 * time.Minute

	msgCancelled = "You cancelled sign-in"
	msgFailed    = "Sign-in didn't complete. Please try again."
	msgSignedOut = "You're signed out. You may still be signed in to Google in this browser."
)

// Options configure the service.
type Options struct {
	Store     *store.Store
	Provider  Provider
	Connector Connector
	Profiler  Profiler
	Checker   Checker
	// CheckInterval and CheckTimeout default to 15 minutes and 3 seconds.
	CheckInterval    time.Duration
	CheckTimeout     time.Duration
	Logger           *slog.Logger
	BaseURL          string
	IdleLifetime     time.Duration
	AbsoluteLifetime time.Duration
}

// Service holds the sign-in handlers and the session gate.
type Service struct {
	Options
	// public lists the routes that need no session. Every other route is signed-in (D6).
	// A later epic adds a public page by adding it here on purpose.
	publicPaths    map[string]bool
	publicPrefixes []string
}

// New builds a Service.
func New(o Options) *Service {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	return &Service{
		Options: o,
		publicPaths: map[string]bool{
			"/signin":               true,
			"/auth/google/start":    true,
			"/auth/google/callback": true,
			// Public so a callback with no session can send the user to sign in (G7).
			"/connect/google/callback": true,
			"/healthz":                 true,
		},
		publicPrefixes: []string{"/static/"},
	}
}

func (s *Service) isPublic(path string) bool {
	if s.publicPaths[path] {
		return true
	}
	for _, p := range s.publicPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// Handler wraps the app's routes. Everything outside the public list needs a session.
func (s *Service) Handler(app http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.Handle("GET /static/", web.Static())
	mux.HandleFunc("GET /signin", s.signInPage)
	mux.HandleFunc("GET /auth/google/start", s.start)
	mux.HandleFunc("GET /auth/google/callback", s.callback)
	mux.HandleFunc("POST /signout", s.signOut)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /connect", s.preConsent)
	mux.HandleFunc("POST /connect/google/start", s.connectStart)
	mux.HandleFunc("GET /connect/google/callback", s.connectCallback)
	mux.Handle("/", app)
	return s.gate(mux)
}

// gate is the single check in front of every signed-in route.
func (s *Service) gate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.isPublic(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		// Signed-in responses, and refusals, must not be stored (G22).
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "Cookie")
		sess, ok := s.session(r)
		if !ok {
			s.refuse(w, r)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)))
	})
}

type sessionKey struct{}

// SessionFrom returns the session the gate resolved for a signed-in request.
func SessionFrom(r *http.Request) (store.Session, bool) {
	sess, ok := r.Context().Value(sessionKey{}).(store.Session)
	return sess, ok
}

func (s *Service) session(r *http.Request) (store.Session, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return store.Session{}, false
	}
	sess, err := s.Store.LookupSession(r.Context(), c.Value, s.IdleLifetime)
	if err != nil {
		if !errors.Is(err, store.ErrNoSession) {
			s.Logger.Error("session lookup failed", "err", err)
		}
		return store.Session{}, false
	}
	return sess, true
}

// refuse sends a signed-out request to the sign-in page. An HTMX request gets a
// full-page redirect, so the sign-in page never lands inside a fragment (G15).
func (s *Service) refuse(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", "/signin")
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/signin", http.StatusSeeOther)
}

type page struct {
	SignedIn bool
	Message  string
	Name     string
	Email    string
}

func (s *Service) signInPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := s.session(r); ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	web.Render(w, http.StatusOK, "signin.html", page{Message: messageFor(r.URL.Query().Get("msg"))})
}

// messageFor maps a fixed key to its text. Query text is never echoed.
func messageFor(key string) string {
	switch key {
	case "cancelled":
		return msgCancelled
	case "failed":
		return msgFailed
	case "signed_out":
		return msgSignedOut
	}
	return ""
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *Service) setCookie(w http.ResponseWriter, name, value string, maxAge time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) failRedirect(w http.ResponseWriter, r *http.Request, key string) {
	s.clearCookie(w, attemptCookie)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/signin?msg="+key, http.StatusSeeOther)
}

// start begins a sign-in attempt and sends the visitor to Google.
func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	binding, err3 := randomToken()
	if err := errors.Join(err1, err2, err3); err != nil {
		s.Logger.Error("sign-in start: random", "err", err)
		s.failRedirect(w, r, "failed")
		return
	}
	verifier := oauth2.GenerateVerifier()
	if err := s.Store.CreateAttempt(r.Context(), state, nonce, verifier, binding, attemptTTL); err != nil {
		s.Logger.Error("sign-in start: store attempt", "err", err)
		s.failRedirect(w, r, "failed")
		return
	}
	dest, err := s.Provider.AuthURL(state, nonce, verifier)
	if err != nil {
		s.Logger.Error("sign-in start: provider", "err", err)
		s.failRedirect(w, r, "failed")
		return
	}
	s.setCookie(w, attemptCookie, binding, attemptTTL)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// callback finishes sign-in. The account and session come last, after every check (D7).
func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var binding string
	if c, err := r.Cookie(attemptCookie); err == nil {
		binding = c.Value
	}
	// Consume the attempt before anything else (G13).
	attempt, attemptErr := s.Store.ConsumeAttempt(r.Context(), q.Get("state"), binding)

	if e := q.Get("error"); e != "" {
		s.Logger.Info("sign-in refused at provider", "error", e)
		if e == "access_denied" {
			s.failRedirect(w, r, "cancelled")
		} else {
			s.failRedirect(w, r, "failed")
		}
		return
	}
	if attemptErr != nil {
		s.Logger.Warn("sign-in failed", "stage", "attempt", "reason", attemptReason(attemptErr))
		s.failRedirect(w, r, "failed")
		return
	}
	code := q.Get("code")
	if code == "" {
		s.Logger.Warn("sign-in failed", "stage", "callback", "reason", "missing code")
		s.failRedirect(w, r, "failed")
		return
	}
	claims, err := s.Provider.Exchange(r.Context(), code, attempt.Verifier, attempt.Nonce)
	if err != nil {
		s.Logger.Warn("sign-in failed", "stage", "exchange", "reason", err.Error())
		s.failRedirect(w, r, "failed")
		return
	}
	accountID, err := s.Store.UpsertAccount(r.Context(), ProviderName, claims)
	if err != nil {
		s.Logger.Error("sign-in failed", "stage", "account", "err", err)
		s.failRedirect(w, r, "failed")
		return
	}
	// A new token for every sign-in; a token set before is never promoted (G9).
	token, err := randomToken()
	if err == nil {
		err = s.Store.CreateSession(r.Context(), token, accountID, s.IdleLifetime, s.AbsoluteLifetime)
	}
	if err != nil {
		s.Logger.Error("sign-in failed", "stage", "session", "err", err)
		s.failRedirect(w, r, "failed")
		return
	}
	s.clearCookie(w, attemptCookie)
	s.setCookie(w, sessionCookie, token, s.AbsoluteLifetime)
	w.Header().Set("Cache-Control", "no-store")
	// Always land on home (D7).
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func attemptReason(err error) string {
	if errors.Is(err, store.ErrNoAttempt) {
		return "missing, mismatched, expired or used state"
	}
	return "store error"
}

// sameOrigin reports whether a state-changing request came from Geronimo itself.
// It trusts Fetch-Metadata when the browser sends it, and falls back to Origin.
func (s *Service) sameOrigin(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin"
	}
	origin := r.Header.Get("Origin")
	return origin != "" && origin == s.BaseURL
}

// signOut ends this browser's session on the server (D8). Other browsers keep theirs,
// and the Google session is untouched. Only a same-origin POST signs out (G20).
func (s *Service) signOut(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.Logger.Warn("sign-out refused", "reason", "not same-origin")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if c, err := r.Cookie(sessionCookie); err == nil {
		// Delete before answering, so the old cookie stops working at once (G19).
		if err := s.Store.DeleteSession(r.Context(), c.Value); err != nil {
			s.Logger.Error("sign-out failed", "err", err)
			http.Error(w, "sign-out failed", http.StatusInternalServerError)
			return
		}
	}
	s.clearCookie(w, sessionCookie)
	// Defence in depth for the back button. Not every browser honours it, so no guarantee depends on it.
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/signin?msg=signed_out", http.StatusSeeOther)
}
