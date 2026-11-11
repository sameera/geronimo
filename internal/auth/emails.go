package auth

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
	"github.com/sameera/geronimo/internal/web"
)

// emailChunk is how many emails one load of a sender's list shows (D8).
const emailChunk = 50

type emailRow struct {
	Href     string // opens the email in the Gmail account that holds it (#43)
	MarkHref string // marks the email read on tap; empty for a read email
	Subject  string
	Unread   bool
	HasDate  bool
	DateISO  string
	DateUTC  string
}

type emailPage struct {
	page
	SenderName    string
	SenderAddress string
	Total         string
	Rows          []emailRow
	MoreHref      string
	BackHref      string
}

// rowFor renders one stored email as a list row.
func rowFor(e store.Email) emailRow {
	row := emailRow{Href: gmail.WebURL(e.MailboxEmail, e.MessageID), Subject: e.Subject, Unread: e.Unread}
	if e.Unread {
		row.MarkHref = "/emails/read?" + url.Values{"mailbox": {strconv.FormatInt(e.MailboxID, 10)}, "message": {e.MessageID}}.Encode()
	}
	if row.Subject == "" {
		row.Subject = "(no subject)"
	}
	if e.Date != store.UnknownDate {
		t := time.Unix(e.Date, 0).UTC()
		row.HasDate, row.DateISO, row.DateUTC = true, t.Format(time.RFC3339), t.Format("2006-01-02")
	}
	return row
}

func encodeCursor(c *store.EmailCursor) string {
	return strconv.FormatInt(c.Date, 10) + ":" + strconv.FormatInt(c.MailboxID, 10) + ":" + c.MessageID
}

func decodeCursor(s string) (*store.EmailCursor, bool) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return nil, false
	}
	d, err1 := strconv.ParseInt(parts[0], 10, 64)
	mb, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil {
		return nil, false
	}
	return &store.EmailCursor{Date: d, MailboxID: mb, MessageID: parts[2]}, true
}

// senderEmails serves a sender's email list. It reads stored mail only, scoped to the signed-in account (D4, G10).
func (s *Service) senderEmails(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r)
	if !ok {
		s.refuse(w, r)
		return
	}
	ctx := r.Context()
	q := r.URL.Query()
	key := q.Get("sender")
	var after *store.EmailCursor
	if a := q.Get("after"); a != "" {
		if after, ok = decodeCursor(a); !ok {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
	}
	emails, next, err := s.Store.SenderEmails(ctx, sess.Account.ID, key, after, emailChunk)
	if err != nil {
		s.Logger.Error("emails: list", "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	data := emailPage{page: page{SignedIn: true, Name: sess.Account.Name, Email: sess.Account.Email}}
	for _, e := range emails {
		data.Rows = append(data.Rows, rowFor(e))
	}
	if next != nil {
		data.MoreHref = "/senders/emails?sender=" + url.QueryEscape(key) + "&after=" + url.QueryEscape(encodeCursor(next))
	}
	view := parseViewState(q)
	data.BackHref = "/senders?" + view.query(view.Shown)
	if next != nil {
		data.MoreHref += "&" + view.query(view.Shown)
	}
	w.Header().Set("Vary", "HX-Request")
	w.Header().Set("Cache-Control", "no-store")
	if r.Header.Get("HX-Request") == "true" {
		web.Render(w, http.StatusOK, "email_rows", data)
		return
	}
	sum, err := s.Store.SenderSummary(ctx, sess.Account.ID, key)
	if err != nil {
		s.Logger.Error("emails: summary", "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	data.SenderName, data.SenderAddress, data.Total = sum.Name, sum.Address, formatCount(sum.Total)+" emails"
	if sum.Total == 1 {
		data.Total = "1 email"
	}
	if data.SenderName == "" {
		data.SenderName = sum.Address
	}
	if data.SenderName == "" {
		data.SenderName = "Unknown sender"
	}
	web.Render(w, http.StatusOK, "emails.html", data)
}
