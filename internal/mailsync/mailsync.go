// Package mailsync runs each connected Gmail mailbox's first sync. It lists the inbox
// newest first, reads each message's metadata, and keeps every step in the store so a
// stopped or restarted sync resumes without storing anything twice.
//
// The sync reaches Gmail only through the access point in internal/gmail, and its
// Gmail interface has no call that changes mail (G10, G11).
package mailsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
)

// ErrAccessRemoved means Google no longer honours the mailbox's grant.
var ErrAccessRemoved = errors.New("access was removed at Google")

const (
	batchSize          = 25
	defaultMaxFailures = 5
	// reasonCancelled ends a run without changing its record. The next start resumes it.
	reasonCancelled = "cancelled"
)

// Tokens turns a stored refresh token into an access token. The access token stays in memory (G20).
type Tokens interface {
	AccessToken(ctx context.Context, refreshToken string) (string, error)
}

// Gmail is the part of the access point a sync uses. It has no call that changes mail.
type Gmail interface {
	InboxCounts(ctx context.Context, token string) (gmail.Counts, error)
	ListInbox(ctx context.Context, token, pageToken string) (gmail.MessagePage, error)
	Metadata(ctx context.Context, token, id string) (gmail.Message, error)
}

// Options configure the worker.
type Options struct {
	Store  *store.Store
	Tokens Tokens
	Gmail  Gmail
	Logger *slog.Logger
	// Backoff returns the wait after a failed attempt. Nil means exponential, capped at a minute.
	Backoff func(attempt int) time.Duration
	// MaxFailures bounds the attempts for one call. Zero means 5.
	MaxFailures int
	// Pace waits between metadata reads, to stay under Gmail's per-user quota. Zero means no wait.
	Pace time.Duration
}

// Worker runs at most one sync per mailbox at a time (G9).
type Worker struct {
	o       Options
	mu      sync.Mutex
	running map[int64]bool
	wg      sync.WaitGroup
}

// New builds a worker.
func New(o Options) *Worker {
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.MaxFailures <= 0 {
		o.MaxFailures = defaultMaxFailures
	}
	if o.Backoff == nil {
		o.Backoff = defaultBackoff
	}
	return &Worker{o: o, running: map[int64]bool{}}
}

func defaultBackoff(attempt int) time.Duration {
	d := time.Second << min(attempt, 6)
	return min(d, time.Minute)
}

// Kick starts the mailbox's sync in the background. A sync already running is not started twice.
func (w *Worker) Kick(mailboxID int64) {
	w.mu.Lock()
	if w.running[mailboxID] {
		w.mu.Unlock()
		return
	}
	w.running[mailboxID] = true
	w.mu.Unlock()
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() {
			w.mu.Lock()
			delete(w.running, mailboxID)
			w.mu.Unlock()
		}()
		w.run(context.Background(), mailboxID)
	}()
}

// Wait blocks until every started sync has returned.
func (w *Worker) Wait() {
	w.wg.Wait()
}

// Resume restarts the syncs a previous run left in starting or running (G8).
func (w *Worker) Resume(ctx context.Context) error {
	recs, err := w.o.Store.ResumableSyncs(ctx)
	if err != nil {
		return err
	}
	for _, r := range recs {
		w.Kick(r.MailboxID)
	}
	return nil
}

func (w *Worker) run(ctx context.Context, mailboxID int64) {
	st := w.o.Store
	rec, err := st.SyncFor(ctx, mailboxID)
	if err != nil {
		w.o.Logger.Error("sync: load record", "err", err)
		return
	}
	if rec.State != store.SyncStarting && rec.State != store.SyncRunning {
		return
	}
	refresh, err := st.RefreshToken(ctx, rec.AccountID, rec.Subject)
	if errors.Is(err, store.ErrUndecryptable) {
		err = fmt.Errorf("%w: stored token cannot be opened", ErrAccessRemoved)
	}
	if err != nil {
		reason := store.StopOther
		if errors.Is(err, ErrAccessRemoved) {
			reason = store.StopAccessRemoved
		}
		w.halt(ctx, rec, reason)
		return
	}
	for ctx.Err() == nil {
		var token string
		if reason := w.try(ctx, func() (err error) {
			token, err = w.o.Tokens.AccessToken(ctx, refresh)
			return err
		}); reason != "" {
			w.halt(ctx, rec, reason)
			return
		}
		finished, reason := w.step(ctx, &rec, token)
		if reason != "" {
			w.halt(ctx, rec, reason)
			return
		}
		if finished {
			w.o.Logger.Info("sync finished")
			return
		}
	}
}

// step does one unit of work. It reports whether the sync finished, or the reason it must stop.
func (w *Worker) step(ctx context.Context, rec *store.SyncRecord, token string) (bool, string) {
	st := w.o.Store
	if rec.State == store.SyncStarting {
		var c gmail.Counts
		if reason := w.try(ctx, func() (err error) {
			c, err = w.o.Gmail.InboxCounts(ctx, token)
			return err
		}); reason != "" {
			return false, reason
		}
		if err := st.StartRun(ctx, rec.MailboxID, c.Total); err != nil {
			w.o.Logger.Error("sync: start run", "err", err)
			return false, store.StopOther
		}
		rec.State = store.SyncRunning
		rec.InboxTotal = c.Total
	}
	pending, err := st.PendingIDs(ctx, rec.MailboxID, batchSize)
	if err != nil {
		w.o.Logger.Error("sync: pending ids", "err", err)
		return false, store.StopOther
	}
	if len(pending) > 0 {
		return false, w.fetch(ctx, rec.MailboxID, token, pending)
	}
	if !rec.ListedAll {
		var page gmail.MessagePage
		if reason := w.try(ctx, func() (err error) {
			page, err = w.o.Gmail.ListInbox(ctx, token, rec.Cursor)
			return err
		}); reason != "" {
			return false, reason
		}
		listedAll := page.Next == ""
		if err := st.StoreListPage(ctx, rec.MailboxID, page.IDs, page.Next, listedAll); err != nil {
			w.o.Logger.Error("sync: store page", "err", err)
			return false, store.StopOther
		}
		rec.Cursor = page.Next
		rec.ListedAll = listedAll
		return false, ""
	}
	// The baseline is Gmail's inbox unread count at the finish, not at the start (G16).
	var c gmail.Counts
	if reason := w.try(ctx, func() (err error) {
		c, err = w.o.Gmail.InboxCounts(ctx, token)
		return err
	}); reason != "" {
		return false, reason
	}
	if err := st.FinishSync(ctx, rec.MailboxID, c.Unread); err != nil {
		w.o.Logger.Error("sync: finish", "err", err)
		return false, store.StopOther
	}
	return true, ""
}

// fetch reads the metadata of pending IDs and stores them in one batch.
func (w *Worker) fetch(ctx context.Context, mailboxID int64, token string, ids []string) string {
	var batch []store.Message
	var gone []string
	for _, id := range ids {
		var m gmail.Message
		var missing bool
		if reason := w.try(ctx, func() error {
			got, err := w.o.Gmail.Metadata(ctx, token, id)
			if gmail.IsNotFound(err) {
				missing = true
				return nil
			}
			m = got
			return err
		}); reason != "" {
			return reason
		}
		if missing {
			gone = append(gone, id)
			continue
		}
		batch = append(batch, store.Message{
			ID:              m.ID,
			ThreadID:        m.ThreadID,
			Sender:          m.From,
			Recipients:      m.To,
			Subject:         m.Subject,
			Date:            m.Date,
			ListID:          m.ListID,
			ListUnsubscribe: m.ListUnsubscribe,
			Labels:          m.Labels,
		})
		if w.o.Pace > 0 {
			select {
			case <-ctx.Done():
				return reasonCancelled
			case <-time.After(w.o.Pace):
			}
		}
	}
	if _, err := w.o.Store.StoreMessages(ctx, mailboxID, batch, gone); err != nil {
		w.o.Logger.Error("sync: store batch", "err", err)
		return store.StopOther
	}
	return ""
}

// try runs fn until it succeeds. It backs off between attempts and gives up after MaxFailures.
// It returns "" on success, or the reason the sync must stop.
func (w *Worker) try(ctx context.Context, fn func() error) string {
	for attempt := 1; ; attempt++ {
		err := fn()
		switch {
		case err == nil:
			return ""
		case errors.Is(err, ErrAccessRemoved):
			return store.StopAccessRemoved
		case ctx.Err() != nil:
			return reasonCancelled
		case attempt >= w.o.MaxFailures:
			w.o.Logger.Warn("sync: gave up", "attempts", attempt, "err", err.Error())
			if gmail.IsRateLimited(err) {
				return store.StopRateLimited
			}
			return store.StopOther
		}
		select {
		case <-ctx.Done():
			return reasonCancelled
		case <-time.After(w.o.Backoff(attempt)):
		}
	}
}

// halt records a stop. An access-removed stop also marks the mailbox, as the connect status does.
func (w *Worker) halt(ctx context.Context, rec store.SyncRecord, reason string) {
	if reason == reasonCancelled {
		return
	}
	bg := context.WithoutCancel(ctx)
	st := w.o.Store
	if reason == store.StopAccessRemoved {
		if err := st.MarkAccessRemoved(bg, rec.AccountID, rec.Subject); err != nil {
			w.o.Logger.Error("sync: mark access removed", "err", err)
		}
	}
	if err := st.StopSync(bg, rec.MailboxID, reason); err != nil {
		w.o.Logger.Error("sync: record stop", "err", err)
		return
	}
	w.o.Logger.Info("sync stopped", "reason", reason)
}
