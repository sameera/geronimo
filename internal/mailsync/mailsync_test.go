package mailsync_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/mailsync"
	"github.com/sameera/geronimo/internal/store"
)

// The sync may use only the Gmail access point's client.
var _ mailsync.Gmail = (*gmail.Client)(nil)

var serverErr = &gmail.StatusError{Code: 500}

// fakeGmail serves inbox pages per access token. hook runs before each call and may fail it.
type fakeGmail struct {
	mu        sync.Mutex
	inbox     map[string][][]string
	counts    map[string]gmail.Counts
	gone      map[string]bool
	hook      func(op, token string, page int) error
	calls     []string
	active    map[string]int
	maxActive map[string]int
}

func newFakeGmail() *fakeGmail {
	return &fakeGmail{
		inbox:     map[string][][]string{},
		counts:    map[string]gmail.Counts{},
		gone:      map[string]bool{},
		active:    map[string]int{},
		maxActive: map[string]int{},
	}
}

func (f *fakeGmail) step(op, token string, page int) error {
	f.mu.Lock()
	f.calls = append(f.calls, op)
	hook := f.hook
	f.mu.Unlock()
	if hook != nil {
		return hook(op, token, page)
	}
	return nil
}

func (f *fakeGmail) enter(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active[token]++
	f.maxActive[token] = max(f.maxActive[token], f.active[token])
}

func (f *fakeGmail) leave(token string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.active[token]--
}

func (f *fakeGmail) InboxCounts(ctx context.Context, token string) (gmail.Counts, error) {
	if err := f.step("counts", token, 0); err != nil {
		return gmail.Counts{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.counts[token], nil
}

func (f *fakeGmail) ListInbox(ctx context.Context, token, pageToken string) (gmail.MessagePage, error) {
	idx := 0
	if pageToken != "" {
		idx, _ = strconv.Atoi(pageToken)
	}
	f.enter(token)
	defer f.leave(token)
	if err := f.step("list", token, idx); err != nil {
		return gmail.MessagePage{}, err
	}
	f.mu.Lock()
	pages := f.inbox[token]
	f.mu.Unlock()
	if idx >= len(pages) {
		return gmail.MessagePage{}, nil
	}
	page := gmail.MessagePage{IDs: slices.Clone(pages[idx])}
	if idx+1 < len(pages) {
		page.Next = strconv.Itoa(idx + 1)
	}
	return page, nil
}

func (f *fakeGmail) Metadata(ctx context.Context, token, id string) (gmail.Message, error) {
	if err := f.step("meta", token, 0); err != nil {
		return gmail.Message{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.gone[id] {
		return gmail.Message{}, &gmail.StatusError{Code: 404}
	}
	return gmail.Message{ID: id, ThreadID: "t-" + id, Labels: []string{"INBOX"}, From: "a@example.com", To: "me@example.com", Subject: "Subject " + id, Date: "Mon, 9 Nov 2026"}, nil
}

func (f *fakeGmail) setInbox(token string, pages ...[]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.inbox[token] = pages
}

func (f *fakeGmail) activeNow(token string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active[token]
}

// fakeTokens turns a refresh token into an access token. "revoked" means Google removed access.
type fakeTokens struct{}

func (fakeTokens) AccessToken(ctx context.Context, refresh string) (string, error) {
	if refresh == "revoked" {
		return "", mailsync.ErrAccessRemoved
	}
	return "access-" + refresh, nil
}

type env struct {
	t    *testing.T
	st   *store.Store
	path string
	gm   *fakeGmail
	acct int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	path := filepath.Join(t.TempDir(), "g.db")
	st, err := store.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SetTokenKey(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, st: st, path: path, gm: newFakeGmail()}
	acct, err := st.UpsertAccount(context.Background(), "google", store.Claims{Subject: "sub-1", Email: "me@example.com", Name: "Me"})
	if err != nil {
		t.Fatal(err)
	}
	e.acct = acct
	return e
}

// mailbox connects a Gmail mailbox the way the connect step does and returns its id.
func (e *env) mailbox(subject, refresh string) int64 {
	e.t.Helper()
	ctx := context.Background()
	if _, err := e.st.SaveMailbox(ctx, e.acct, store.Mailbox{Subject: subject, Email: subject + "@example.com", RefreshToken: refresh, Scopes: []string{gmail.ModifyScope}}); err != nil {
		e.t.Fatal(err)
	}
	id, err := e.st.MailboxID(ctx, e.acct, subject)
	if err != nil {
		e.t.Fatal(err)
	}
	return id
}

func (e *env) worker() *mailsync.Worker {
	return mailsync.New(mailsync.Options{
		Store:       e.st,
		Tokens:      fakeTokens{},
		Gmail:       e.gm,
		Backoff:     func(int) time.Duration { return 0 },
		MaxFailures: 3,
	})
}

// start records the connect step's sync record and starts the worker without waiting.
func (e *env) start(w *mailsync.Worker, id int64) {
	e.t.Helper()
	if err := e.st.EnsureSync(context.Background(), id); err != nil {
		e.t.Fatal(err)
	}
	w.Kick(id)
}

func (e *env) sync(id int64) store.SyncRecord {
	e.t.Helper()
	rec, err := e.st.SyncFor(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return rec
}

func (e *env) messages(id int64) []string {
	e.t.Helper()
	ids, err := e.st.MessageIDs(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return ids
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestSyncStartsOnConnectAndFinishes(t *testing.T) { // G1
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m3", "m2"}, []string{"m1"})
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncDone {
		t.Fatalf("state = %s, reason %q", rec.State, rec.StopReason)
	}
	if got := e.messages(id); strings.Join(got, ",") != "m3,m2,m1" {
		t.Fatalf("messages = %v", got)
	}
}

func TestNewestInboxMailIsStoredFirst(t *testing.T) { // G3
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"new2", "new1"}, []string{"old"})
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if got := e.messages(id); got[0] != "new2" {
		t.Fatalf("first stored = %v", got)
	}
}

func TestHomeOpensWhileTheSyncRuns(t *testing.T) { // G2
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m2", "m1"}, []string{"m0"})
	release := make(chan struct{})
	e.gm.hook = func(op, token string, page int) error {
		if op == "list" && page == 1 {
			<-release
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	waitFor(t, "first batch", func() bool { return len(e.messages(id)) == 2 })
	if rec := e.sync(id); rec.State != store.SyncRunning {
		t.Fatalf("state while waiting = %s", rec.State)
	}
	close(release)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncDone {
		t.Fatalf("state after release = %s", rec.State)
	}
}

func TestOneAccountFailingLeavesTheOtherSyncing(t *testing.T) { // G4
	e := newEnv(t)
	a := e.mailbox("a", "refresh-a")
	b := e.mailbox("b", "refresh-b")
	e.gm.setInbox("access-refresh-a", []string{"a1"})
	e.gm.setInbox("access-refresh-b", []string{"b2", "b1"}, []string{"b0"})
	e.gm.hook = func(op, token string, page int) error {
		if token == "access-refresh-a" && op == "list" {
			return serverErr
		}
		return nil
	}
	w := e.worker()
	e.start(w, a)
	e.start(w, b)
	w.Wait()
	if rec := e.sync(a); rec.State != store.SyncStopped || rec.StopReason != store.StopOther {
		t.Fatalf("failing account = %s / %q", rec.State, rec.StopReason)
	}
	if rec := e.sync(b); rec.State != store.SyncDone {
		t.Fatalf("other account = %s", rec.State)
	}
	if got := e.messages(b); len(got) != 3 {
		t.Fatalf("other account messages = %v", got)
	}
}

func TestStoppedSyncKeepsWhatItStored(t *testing.T) { // G5
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m2", "m1"}, []string{"m0"})
	e.gm.hook = func(op, token string, page int) error {
		if op == "list" && page == 1 {
			return serverErr
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncStopped {
		t.Fatalf("state = %s", rec.State)
	}
	if got := e.messages(id); strings.Join(got, ",") != "m2,m1" {
		t.Fatalf("kept messages = %v", got)
	}
}

func TestRetryContinuesWithoutDuplicates(t *testing.T) { // G6, G7
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m2", "m1"}, []string{"m0"})
	broken := true
	e.gm.hook = func(op, token string, page int) error {
		if op == "list" && page == 1 && broken {
			return serverErr
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncStopped || rec.StopReason != store.StopOther {
		t.Fatalf("first run = %s / %q", rec.State, rec.StopReason)
	}
	broken = false
	if err := e.st.EnsureSync(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	w2 := e.worker()
	w2.Kick(id)
	w2.Wait()
	if rec := e.sync(id); rec.State != store.SyncDone {
		t.Fatalf("retried run = %s", rec.State)
	}
	if got := e.messages(id); strings.Join(got, ",") != "m2,m1,m0" {
		t.Fatalf("messages after retry = %v", got)
	}
}

func TestRateLimitBacksOffThenContinues(t *testing.T) { // D5
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m1"})
	limited := true
	e.gm.hook = func(op, token string, page int) error {
		if op == "list" && limited {
			limited = false
			return &gmail.StatusError{Code: 429}
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncDone {
		t.Fatalf("state = %s / %q", rec.State, rec.StopReason)
	}
}

func TestRepeatedRateLimitStopsWithItsReason(t *testing.T) { // D5
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m1"})
	e.gm.hook = func(op, token string, page int) error {
		if op == "list" {
			return &gmail.StatusError{Code: 429}
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncStopped || rec.StopReason != store.StopRateLimited {
		t.Fatalf("state = %s / %q", rec.State, rec.StopReason)
	}
}

func TestRevokedAccessStopsAndMarksTheMailbox(t *testing.T) { // D4, D5, G6
	e := newEnv(t)
	id := e.mailbox("a", "revoked")
	e.gm.setInbox("access-revoked", []string{"m1"})
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncStopped || rec.StopReason != store.StopAccessRemoved {
		t.Fatalf("state = %s / %q", rec.State, rec.StopReason)
	}
	boxes, err := e.st.Mailboxes(context.Background(), e.acct)
	if err != nil || len(boxes) != 1 || !boxes[0].AccessRemoved() {
		t.Fatalf("mailboxes = %+v, %v", boxes, err)
	}
}

func TestResumeRestartsStartedSyncsAndKeepsStoppedOnes(t *testing.T) { // G8
	e := newEnv(t)
	ctx := context.Background()
	a := e.mailbox("a", "refresh-a")
	b := e.mailbox("b", "refresh-b")
	e.gm.setInbox("access-refresh-a", []string{"m1"})
	e.gm.setInbox("access-refresh-b", []string{"n1"})
	if err := e.st.EnsureSync(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := e.st.EnsureSync(ctx, b); err != nil {
		t.Fatal(err)
	}
	if err := e.st.StopSync(ctx, b, store.StopOther); err != nil {
		t.Fatal(err)
	}
	w := e.worker()
	if err := w.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	w.Wait()
	if rec := e.sync(a); rec.State != store.SyncDone {
		t.Fatalf("started sync after restart = %s", rec.State)
	}
	if rec := e.sync(b); rec.State != store.SyncStopped {
		t.Fatalf("stopped sync after restart = %s", rec.State)
	}
	if got := e.messages(b); len(got) != 0 {
		t.Fatalf("stopped sync ran: %v", got)
	}
}

func TestOneMailboxNeverSyncsTwiceAtOnce(t *testing.T) { // G9
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m2"}, []string{"m1"})
	release := make(chan struct{})
	e.gm.hook = func(op, token string, page int) error {
		if op == "list" && page == 1 {
			<-release
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	waitFor(t, "first run blocked", func() bool { return e.gm.activeNow("access-refresh-a") == 1 })
	w.Kick(id)
	w.Kick(id)
	close(release)
	w.Wait()
	e.gm.mu.Lock()
	peak := e.gm.maxActive["access-refresh-a"]
	e.gm.mu.Unlock()
	if peak != 1 {
		t.Fatalf("peak concurrent listings = %d", peak)
	}
	if rec := e.sync(id); rec.State != store.SyncDone {
		t.Fatalf("state = %s", rec.State)
	}
}

func TestAccessTokensNeverReachTheDisk(t *testing.T) { // G20
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m1"})
	w := e.worker()
	e.start(w, id)
	w.Wait()
	for _, p := range []string{e.path, e.path + "-wal"} {
		b, err := os.ReadFile(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"access-refresh-a", "refresh-a"} {
			if strings.Contains(string(b), secret) {
				t.Errorf("%s holds %q in plain text", filepath.Base(p), secret)
			}
		}
	}
}
