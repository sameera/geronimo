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
	"runtime/debug"
	"sync"
	"time"

	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
)

// ErrAccessRemoved means Google no longer honours the mailbox's grant.
var ErrAccessRemoved = errors.New("access was removed at Google")

const (
	batchSize = 25
	// fetchConcurrency bounds the metadata reads in flight for one mailbox.
	fetchConcurrency   = 5
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
	// rerun marks a mailbox kicked while its sync was still running. Its goroutine runs once more.
	rerun map[int64]bool
	wg    sync.WaitGroup
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
	return &Worker{o: o, running: map[int64]bool{}, rerun: map[int64]bool{}}
}

func defaultBackoff(attempt int) time.Duration {
	d := time.Second << min(attempt, 6)
	return min(d, time.Minute)
}

// Kick starts the mailbox's sync in the background. A sync already running is not started twice.
// A Kick that lands while the mailbox's goroutine is still exiting is run again before it exits.
func (w *Worker) Kick(mailboxID int64) {
	w.mu.Lock()
	if w.running[mailboxID] {
		w.rerun[mailboxID] = true
		w.mu.Unlock()
		return
	}
	w.running[mailboxID] = true
	w.mu.Unlock()
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		for {
			w.runSafely(context.Background(), mailboxID)
			w.mu.Lock()
			if !w.rerun[mailboxID] {
				delete(w.running, mailboxID)
				w.mu.Unlock()
				return
			}
			delete(w.rerun, mailboxID)
			w.mu.Unlock()
		}
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

// runSafely runs one sync. A panic stops only this mailbox, with a stop recorded and logged (G4).
func (w *Worker) runSafely(ctx context.Context, mailboxID int64) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		w.o.Logger.Error("sync: panic", "mailbox", mailboxID, "panic", fmt.Sprintf("%T", p), "stack", string(debug.Stack()))
		if err := w.o.Store.StopSync(context.WithoutCancel(ctx), mailboxID, store.StopOther); err != nil {
			w.o.Logger.Error("sync: record panic stop", "err", err)
		}
	}()
	w.run(ctx, mailboxID)
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
		w.halt(ctx, rec, store.StopTokenUnreadable)
		return
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
			if gmail.IsBadRequest(err) && rec.Cursor != "" {
				// Gmail rejected the saved position. List again from the top. Stored IDs are skipped (D5).
				w.o.Logger.Warn("sync: listing position rejected, listing from the top")
				rec.Cursor = ""
				page, err = w.o.Gmail.ListInbox(ctx, token, "")
			}
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

// fetch reads the metadata of pending IDs, a few at a time, and stores them in one batch.
// Reads share one pacing gate, so the per-user quota holds however many run at once (D5).
func (w *Worker) fetch(ctx context.Context, mailboxID int64, token string, ids []string) string {
	type result struct {
		msg     store.Message
		missing bool
		reason  string
	}
	results := make([]result, len(ids))
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var gate sync.Mutex
	var next time.Time
	// wait reserves the next read slot, so reads start at most one Pace apart.
	wait := func() string {
		if w.o.Pace <= 0 {
			return ""
		}
		gate.Lock()
		now := time.Now()
		if next.Before(now) {
			next = now
		}
		at := next
		next = next.Add(w.o.Pace)
		gate.Unlock()
		select {
		case <-runCtx.Done():
			return reasonCancelled
		case <-time.After(time.Until(at)):
			return ""
		}
	}
	sem := make(chan struct{}, fetchConcurrency)
	var wg sync.WaitGroup
	for i, id := range ids {
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			// A panic in one read stops only this mailbox, as runSafely does for the run (G4).
			defer func() {
				p := recover()
				if p == nil {
					return
				}
				w.o.Logger.Error("sync: panic in read", "mailbox", mailboxID, "panic", fmt.Sprintf("%T", p), "stack", string(debug.Stack()))
				results[i].reason = store.StopOther
				cancel()
			}()
			if reason := wait(); reason != "" {
				results[i].reason = reason
				return
			}
			var m gmail.Message
			reason := w.try(runCtx, func() error {
				got, err := w.o.Gmail.Metadata(runCtx, token, id)
				if gmail.IsNotFound(err) {
					results[i].missing = true
					return nil
				}
				m = got
				return err
			})
			if reason != "" {
				results[i].reason = reason
				cancel()
				return
			}
			results[i].msg = store.Message{
				ID:              m.ID,
				ThreadID:        m.ThreadID,
				Sender:          m.From,
				Recipients:      m.To,
				Subject:         m.Subject,
				Date:            m.Date,
				ListID:          m.ListID,
				ListUnsubscribe: m.ListUnsubscribe,
				Labels:          m.Labels,
			}
		}()
	}
	wg.Wait()
	// Report the first real failure. A cancelled read is a side effect of it, unless the caller cancelled.
	stop := ""
	for _, r := range results {
		if r.reason != "" && (stop == "" || stop == reasonCancelled) {
			stop = r.reason
		}
	}
	if stop != "" {
		return stop
	}
	var batch []store.Message
	var gone []string
	for i, r := range results {
		if r.missing {
			gone = append(gone, ids[i])
			continue
		}
		batch = append(batch, r.msg)
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
