package auth

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/sameera/geronimo/internal/store"
	"github.com/sameera/geronimo/internal/web"
)

// markReadTimeout bounds the token refresh and the Gmail request behind one tap.
const markReadTimeout = 10 * time.Second

// TokenSource turns a stored refresh token into an access token, kept in memory only.
type TokenSource interface {
	AccessToken(ctx context.Context, refreshToken string) (string, error)
}

// MailWriter is the Gmail access point's one write: removing a message's UNREAD label.
type MailWriter interface {
	MarkRead(ctx context.Context, accessToken, id string) error
}

type markReadResult struct {
	Row    emailRow
	Notice string
}

// markRead marks a tapped email read in Gmail, then in the store (#43). The tap's link opens Gmail
// whatever happens here; a refused read mark leaves the email unread and tells the user.
func (s *Service) markRead(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r)
	if !ok {
		s.refuse(w, r)
		return
	}
	if !s.sameOrigin(r) {
		s.Logger.Warn("mark read refused", "reason", "not same-origin")
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	mailboxID, err := strconv.ParseInt(r.FormValue("mailbox"), 10, 64)
	messageID := r.FormValue("message")
	if err != nil || messageID == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	email, subject, err := s.Store.EmailFor(ctx, sess.Account.ID, mailboxID, messageID)
	if errors.Is(err, store.ErrNoEmail) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.Logger.Error("mark read: load email", "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	var res markReadResult
	if err := s.markInGmail(ctx, sess.Account.ID, subject, messageID); err != nil {
		s.Logger.Warn("mark read: not saved in gmail", "err", err.Error())
		label := email.Subject
		if label == "" {
			label = "(no subject)"
		}
		res.Notice = "The read mark for “" + label + "” was not saved in Gmail. It still shows as unread."
	} else {
		email.Unread = false
		if err := s.Store.MarkEmailRead(ctx, mailboxID, messageID); err != nil {
			s.Logger.Error("mark read: store", "err", err)
		}
	}
	res.Row = rowFor(email)
	w.Header().Set("Cache-Control", "no-store")
	web.Render(w, http.StatusOK, "email_read", res)
}

func (s *Service) markInGmail(ctx context.Context, accountID int64, subject, messageID string) error {
	if s.Tokens == nil || s.Mail == nil {
		return errors.New("mark read is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, markReadTimeout)
	defer cancel()
	refresh, err := s.Store.RefreshToken(ctx, accountID, subject)
	if err != nil {
		return err
	}
	token, err := s.Tokens.AccessToken(ctx, refresh)
	if err != nil {
		return err
	}
	return s.Mail.MarkRead(ctx, token, messageID)
}
