package auth

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/sameera/geronimo/internal/store"
	"github.com/sameera/geronimo/internal/web"
)

// strip is the progress strip on home. It has one line per syncing account and shows
// a done line once (G15). Poll asks HTMX to refresh the strip every two seconds.
type strip struct {
	Poll  bool
	Lines []string
	// Ack lists the mailboxes whose done line this render shows. They are acknowledged once rendered.
	Ack []int64
}

// buildStrip turns an account's sync records into the strip. A stopped sync is not
// here: its plain message and action are a notice on home.
func buildStrip(recs []store.SyncRecord) strip {
	var v strip
	for _, r := range recs {
		switch {
		case r.State == store.SyncStarting:
			v.Lines = append(v.Lines, "Starting sync of "+r.Email+"...")
			v.Poll = true
		case r.State == store.SyncRunning:
			// The total can lag behind mail that arrives during the sync, so it never shows below the synced count (D7).
			total := max(r.InboxTotal, r.SyncedCount)
			v.Lines = append(v.Lines, "Syncing "+r.Email+": "+formatCount(r.SyncedCount)+" of "+formatCount(total)+" emails")
			v.Poll = true
		case r.State == store.SyncDone && !r.StripAcked:
			v.Lines = append(v.Lines, "Sync done for "+r.Email+".")
			v.Ack = append(v.Ack, r.MailboxID)
			v.Poll = true
		}
	}
	return v
}

// stripFrom builds the strip and acknowledges the done lines it shows, so they go away on the next poll (G15).
func (s *Service) stripFrom(ctx context.Context, recs []store.SyncRecord) strip {
	v := buildStrip(recs)
	for _, id := range v.Ack {
		if err := s.Store.MarkStripAcked(ctx, id); err != nil {
			s.Logger.Error("strip: acknowledge", "err", err)
		}
	}
	return v
}

// syncStrip serves the strip fragment that home polls while a sync runs.
func (s *Service) syncStrip(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFrom(r)
	if !ok {
		s.refuse(w, r)
		return
	}
	recs, err := s.Store.AccountSyncs(r.Context(), sess.Account.ID)
	if err != nil {
		s.Logger.Error("strip: list syncs", "err", err)
		http.Error(w, "something went wrong", http.StatusInternalServerError)
		return
	}
	web.Render(w, http.StatusOK, "strip", s.stripFrom(r.Context(), recs))
}

// formatCount writes n with a comma every three digits, as in "12,400".
func formatCount(n int) string {
	s := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
