package auth

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sync"
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
	keyDenied      = "connect_denied"
	keyFailed      = "connect_failed"
	keyPartial     = "connect_partial"
	keyAlready     = "connect_already"
	keyReconnected = "connect_reconnected"

	// Status checks on open (decision D8).
	defaultCheckInterval = 15 * time.Minute
	defaultCheckTimeout  = 3 * time.Second
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
	case keyAlready:
		return homeMessage{"That Gmail account is already connected.", false}, true
	case keyReconnected:
		return homeMessage{"Gmail account reconnected.", false}, true
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
	// ShowConnect shows the pre-consent screen even when accounts are connected (G24).
	ShowConnect bool
}

// home shows the pre-consent screen to a user with no connected Gmail, and the
// connected accounts to a user with some (decision D10).
func (s *Service) home(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.checkMailboxes(r.Context(), sess.Account.ID)
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

// preConsent is where retry and reconnect lead (G24). It shows the pre-consent
// screen whether or not accounts are connected.
func (s *Service) preConsent(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r)
	if !ok {
		s.refuse(w, r)
		return
	}
	web.Render(w, http.StatusOK, "home.html", homePage{
		page:        page{SignedIn: true, Name: sess.Account.Name, Email: sess.Account.Email},
		ShowConnect: true,
	})
}

// checkMailboxes tests that each connected mailbox's token still works, at most once
// per interval per mailbox, and never holds the page past the timeout. Only Google's
// explicit answer or an unreadable token marks access removed (G18, G22).
func (s *Service) checkMailboxes(ctx context.Context, accountID int64) {
	if s.Checker == nil {
		return
	}
	interval, timeout := s.CheckInterval, s.CheckTimeout
	if interval <= 0 {
		interval = defaultCheckInterval
	}
	if timeout <= 0 {
		timeout = defaultCheckTimeout
	}
	due, err := s.Store.ClaimDueChecks(ctx, accountID, interval)
	if err != nil {
		s.Logger.Error("status check: claim", "err", err)
		return
	}
	if len(due) == 0 {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var wg sync.WaitGroup
	for _, subject := range due {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.checkOne(cctx, accountID, subject)
		}()
	}
	wg.Wait()
}

func (s *Service) checkOne(ctx context.Context, accountID int64, subject string) {
	token, err := s.Store.RefreshToken(ctx, accountID, subject)
	if err == nil {
		err = s.Checker.Check(ctx, token)
		token = ""
	}
	switch {
	case err == nil:
	case errors.Is(err, ErrAccessRemoved) || errors.Is(err, store.ErrUndecryptable):
		s.Logger.Info("status check: access removed", "reason", err.Error())
		// The check's context may be spent, so the write uses its own.
		if err := s.Store.MarkAccessRemoved(context.WithoutCancel(ctx), accountID, subject); err != nil {
			s.Logger.Error("status check: mark removed", "err", err)
		}
	default:
		// Unknown outcome: leave the status as it is and let the page render.
		s.Logger.Warn("status check: no answer", "reason", err.Error())
	}
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
	outcome, err := s.Store.SaveMailbox(r.Context(), sess.Account.ID, store.Mailbox{
		Subject: grant.Subject, Email: grant.Email, RefreshToken: grant.RefreshToken, Scopes: grant.Scopes,
	})
	if err != nil {
		s.Logger.Error("connect failed", "stage", "store", "err", err)
		s.connectFail(w, r, keyFailed)
		return
	}
	s.clearCookie(w, connectCookie)
	w.Header().Set("Cache-Control", "no-store")
	switch outcome {
	case store.Replaced:
		http.Redirect(w, r, "/?msg="+keyAlready, http.StatusSeeOther)
		return
	case store.Reconnected:
		http.Redirect(w, r, "/?msg="+keyReconnected, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
