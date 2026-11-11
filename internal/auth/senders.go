package auth

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sameera/geronimo/internal/store"
	"github.com/sameera/geronimo/internal/web"
)

const (
	// groupChunk is how many sender groups one load adds (D5).
	groupChunk = 100
	// maxShown bounds the shown count taken from the page address.
	maxShown = 100000
)

// groupRow is one sender group row. Names are plain text; the template escapes them (G23).
type groupRow struct {
	Href      string
	Name      string
	Address   string
	Total     string
	Unread    string
	HasNewest bool
	NewestISO string
	NewestUTC string
}

type senderPage struct {
	page
	Strip     strip
	Rows      []groupRow
	Empty     bool
	EmptyText string
	Connect   bool
	MoreHref  string
}

// viewState is the sender view's state, carried in its page address (D5).
type viewState struct {
	Shown int
}

func parseViewState(q url.Values) viewState {
	v := viewState{Shown: groupChunk}
	if n, err := strconv.Atoi(q.Get("n")); err == nil && n > 0 {
		v.Shown = min(n, maxShown)
	}
	return v
}

func (v viewState) query(shown int) string {
	return "n=" + strconv.Itoa(shown)
}

// senders serves the sender view. It reads stored mail only: no Gmail call, no write (D4, G11).
func (s *Service) senders(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r)
	if !ok {
		s.refuse(w, r)
		return
	}
	ctx := r.Context()
	view := parseViewState(r.URL.Query())
	groups, total, err := s.Store.SenderGroups(ctx, sess.Account.ID, store.SortTotal, view.Shown)
	if err != nil {
		s.Logger.Error("senders: groups", "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	syncs, err := s.Store.AccountSyncs(ctx, sess.Account.ID)
	if err != nil {
		s.Logger.Error("senders: syncs", "err", err)
	}
	boxes, err := s.Store.Mailboxes(ctx, sess.Account.ID)
	if err != nil {
		s.Logger.Error("senders: mailboxes", "err", err)
	}
	data := senderPage{page: page{SignedIn: true, Name: sess.Account.Name, Email: sess.Account.Email}}
	for _, g := range groups {
		row := groupRow{
			Href:    "/senders/emails?sender=" + url.QueryEscape(g.Key),
			Name:    g.Name,
			Address: g.Address,
			Total:   formatCount(g.Total),
			Unread:  formatCount(g.Unread),
		}
		if row.Name == "" {
			row.Name = g.Address
		}
		if row.Name == "" {
			row.Name = "Unknown sender"
		}
		if t, ok := g.Time(); ok {
			row.HasNewest = true
			row.NewestISO = t.Format(time.RFC3339)
			row.NewestUTC = t.Format("2006-01-02")
		}
		data.Rows = append(data.Rows, row)
	}
	if total > len(groups) {
		data.MoreHref = "/senders?" + view.query(view.Shown+groupChunk)
	}
	if len(data.Rows) == 0 {
		data.Empty = true
		if len(boxes) == 0 {
			data.Connect = true
			data.EmptyText = "No Gmail account is connected yet."
		} else {
			data.EmptyText = "Your mail is still syncing. Senders appear here as mail arrives."
		}
	}
	if r.Header.Get("HX-Request") == "true" {
		web.Render(w, http.StatusOK, "sender_list", data)
		return
	}
	data.Strip = s.stripFrom(ctx, syncs)
	web.Render(w, http.StatusOK, "senders.html", data)
}
