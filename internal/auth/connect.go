package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"golang.org/x/oauth2"

	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
	"github.com/sameera/geronimo/internal/web"
)

const (
	connectCookie = "__Host-geronimo_connect"
	connectTTL    = 5 * time.Minute

	// Message keys shown on home. Query text is never echoed (G33).
	keyDenied  = "connect_denied"
	keyFailed  = "connect_failed"
	keyPartial = "connect_partial"
)

// Profiler makes the Gmail profile request through the Gmail access point.
type Profiler interface {
	Profile(ctx context.Context, accessToken string) (gmail.Profile, error)
}

type homeMessage struct {
	Text  string
	Retry bool // the message offers a retry that leads to the pre-consent screen
}

func homeMessageFor(key string) (homeMessage, bool) {
	switch key {
	case keyDenied:
		return homeMessage{"Gmail was not connected. You can try again.", true}, true
	case keyFailed:
		return homeMessage{"Gmail wasn't connected, please try again.", true}, true
	case keyPartial:
		return homeMessage{"Gmail wasn't connected. Geronimo needs permission to read and manage your mail, and that permission wasn't granted. Please try again.", true}, true
	}
	return homeMessage{}, false
}

type homePage struct {
	page
	Notice    homeMessage
	HasNotice bool
	Mailboxes []store.MailboxInfo
}

// home shows the pre-consent screen to a user with no connected Gmail, and the
// connected accounts to a user with some (decision D10).
func (s *Service) home(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	boxes, err := s.Store.Mailboxes(r.Context(), sess.Account.ID)
	if err != nil {
		s.Logger.Error("home: list mailboxes", "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	data := homePage{
		page:      page{SignedIn: true, Name: sess.Account.Name, Email: sess.Account.Email},
		Mailboxes: boxes,
	}
	data.Notice, data.HasNotice = homeMessageFor(r.URL.Query().Get("msg"))
	web.Render(w, http.StatusOK, "home.html", data)
}

func (s *Service) connectFail(w http.ResponseWriter, r *http.Request, key string) {
	s.clearCookie(w, connectCookie)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/?msg="+key, http.StatusSeeOther)
}

// connectStart begins a connect attempt and sends the signed-in user to Google.
// Only a same-origin POST by a signed-in user starts one (G5).
func (s *Service) connectStart(w http.ResponseWriter, r *http.Request) {
	if !s.sameOrigin(r) {
		s.Logger.Warn("connect start refused", "reason", "not same-origin")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	sess, ok := SessionFrom(r)
	if !ok {
		s.refuse(w, r)
		return
	}
	state, err1 := randomToken()
	nonce, err2 := randomToken()
	binding, err3 := randomToken()
	if err := errors.Join(err1, err2, err3); err != nil {
		s.Logger.Error("connect start: random", "err", err)
		s.connectFail(w, r, keyFailed)
		return
	}
	verifier := oauth2.GenerateVerifier()
	if err := s.Store.CreateConnectAttempt(r.Context(), state, nonce, verifier, binding, sess.Account.ID, connectTTL); err != nil {
		s.Logger.Error("connect start: store attempt", "err", err)
		s.connectFail(w, r, keyFailed)
		return
	}
	dest, err := s.Connector.AuthURL(state, nonce, verifier)
	if err != nil {
		s.Logger.Error("connect start: provider", "err", err)
		s.connectFail(w, r, keyFailed)
		return
	}
	s.setCookie(w, connectCookie, binding, connectTTL)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

// connectCallback finishes a connect. The mailbox is stored last, after every check (D3, D4, D7).
func (s *Service) connectCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var binding string
	if c, err := r.Cookie(connectCookie); err == nil {
		binding = c.Value
	}
	// Consume the attempt before anything else, so it works at most once (G6).
	attempt, attemptErr := s.Store.ConsumeConnectAttempt(r.Context(), q.Get("state"), binding)
	if attemptErr != nil {
		s.Logger.Warn("connect failed", "stage", "attempt", "reason", attemptReason(attemptErr))
		s.connectFail(w, r, keyFailed)
		return
	}
	if e := q.Get("error"); e != "" {
		s.Logger.Info("connect refused at provider", "error", e)
		if e == "access_denied" {
			s.connectFail(w, r, keyDenied)
		} else {
			s.connectFail(w, r, keyFailed)
		}
		return
	}
	// No session: exchange nothing, connect nothing, send the user to sign in (G7).
	sess, ok := s.session(r)
	if !ok {
		s.clearCookie(w, connectCookie)
		w.Header().Set("Cache-Control", "no-store")
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return
	}
	// The signed-in account must be the one that started the connect (G8).
	if sess.Account.ID != attempt.AccountID {
		s.Logger.Warn("connect failed", "stage", "account", "reason", "signed-in account differs from the one that started the connect")
		s.connectFail(w, r, keyFailed)
		return
	}
	code := q.Get("code")
	if code == "" {
		s.Logger.Warn("connect failed", "stage", "callback", "reason", "missing code")
		s.connectFail(w, r, keyFailed)
		return
	}
	grant, err := s.Connector.Exchange(r.Context(), code, attempt.Verifier, attempt.Nonce)
	if err != nil {
		s.Logger.Warn("connect failed", "stage", "exchange", "reason", err.Error())
		s.connectFail(w, r, keyFailed)
		return
	}
	// A partial grant stores nothing and is never revoked at Google (G10, D4).
	if !slices.Contains(grant.Scopes, gmail.ModifyScope) {
		s.Logger.Info("connect refused", "stage", "scopes", "reason", "gmail modify not granted")
		s.connectFail(w, r, keyPartial)
		return
	}
	if _, err := s.Profiler.Profile(r.Context(), grant.AccessToken); err != nil {
		s.Logger.Warn("connect failed", "stage", "profile", "reason", err.Error())
		s.connectFail(w, r, keyFailed)
		return
	}
	if err := s.Store.SaveMailbox(r.Context(), sess.Account.ID, store.Mailbox{
		Subject: grant.Subject, Email: grant.Email, RefreshToken: grant.RefreshToken, Scopes: grant.Scopes,
	}); err != nil {
		s.Logger.Error("connect failed", "stage", "store", "err", err)
		s.connectFail(w, r, keyFailed)
		return
	}
	s.clearCookie(w, connectCookie)
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
