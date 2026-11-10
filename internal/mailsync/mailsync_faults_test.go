package mailsync_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
)

func TestPanicInOneSyncStopsOnlyThatAccount(t *testing.T) { // G4, #37 AC3
	e := newEnv(t)
	a := e.mailbox("a", "refresh-a")
	b := e.mailbox("b", "refresh-b")
	e.gm.setInbox("access-refresh-a", []string{"a1"})
	e.gm.setInbox("access-refresh-b", []string{"b2", "b1"}, []string{"b0"})
	e.gm.hook = func(op, token string, page int) error {
		if token == "access-refresh-a" && op == "list" {
			panic("boom")
		}
		return nil
	}
	w := e.worker()
	e.start(w, a)
	e.start(w, b)
	w.Wait()
	if rec := e.sync(a); rec.State != store.SyncStopped || rec.StopReason != store.StopOther {
		t.Fatalf("panicking account = %s / %q", rec.State, rec.StopReason)
	}
	if rec := e.sync(b); rec.State != store.SyncDone {
		t.Fatalf("other account = %s", rec.State)
	}
}

func TestPanicInAMetadataReadStopsOnlyThatAccount(t *testing.T) { // G4, #37 AC3, F10
	e := newEnv(t)
	a := e.mailbox("a", "refresh-a")
	b := e.mailbox("b", "refresh-b")
	e.gm.setInbox("access-refresh-a", []string{"a3", "a2", "a1"})
	e.gm.setInbox("access-refresh-b", []string{"b2", "b1"}, []string{"b0"})
	e.gm.hook = func(op, token string, page int) error {
		if token == "access-refresh-a" && op == "meta" {
			panic("boom")
		}
		return nil
	}
	w := e.worker()
	e.start(w, a)
	e.start(w, b)
	w.Wait()
	if rec := e.sync(a); rec.State != store.SyncStopped || rec.StopReason != store.StopOther {
		t.Fatalf("panicking account = %s / %q", rec.State, rec.StopReason)
	}
	if rec := e.sync(b); rec.State != store.SyncDone {
		t.Fatalf("other account = %s", rec.State)
	}
}

func TestUndecryptableTokenHasItsOwnStopReason(t *testing.T) { // G6, #37 AC4
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m1"})
	// A different key cannot open the token that was stored under the first key.
	other := make([]byte, 32)
	for i := range other {
		other[i] = 9
	}
	if err := e.st.SetTokenKey(other); err != nil {
		t.Fatal(err)
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncStopped || rec.StopReason != store.StopTokenUnreadable {
		t.Fatalf("state = %s / %q", rec.State, rec.StopReason)
	}
	boxes, err := e.st.Mailboxes(context.Background(), e.acct)
	if err != nil || len(boxes) != 1 || boxes[0].AccessRemoved() {
		t.Fatalf("local key fault marked access removed: %+v, %v", boxes, err)
	}
}

func TestRejectedListingPositionListsAgainFromTheTop(t *testing.T) { // D5, DV3
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m2", "m1"})
	e.gm.hook = func(op, token string, page int) error {
		if op == "list" && page == 99 {
			return &gmail.StatusError{Code: 400}
		}
		return nil
	}
	ctx := context.Background()
	if err := e.st.EnsureSync(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := e.st.StartRun(ctx, id, 2); err != nil {
		t.Fatal(err)
	}
	if err := e.st.StoreListPage(ctx, id, []string{"m2"}, "99", false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.StoreMessages(ctx, id, []store.Message{{ID: "m2"}}, nil); err != nil {
		t.Fatal(err)
	}
	w := e.worker()
	w.Kick(id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncDone {
		t.Fatalf("state = %s, reason %q", rec.State, rec.StopReason)
	}
	if got := strings.Join(e.messages(id), ","); got != "m2,m1" {
		t.Fatalf("messages = %s", got)
	}
}

func TestMetadataReadsRunAFewAtATime(t *testing.T) { // D5, DV4
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	e.gm.setInbox("access-refresh-a", []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8"})
	var mu sync.Mutex
	var now, peak int
	e.gm.hook = func(op, token string, page int) error {
		if op != "meta" {
			return nil
		}
		mu.Lock()
		now++
		peak = max(peak, now)
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		now--
		mu.Unlock()
		return nil
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if got := strings.Join(e.messages(id), ","); got != "m1,m2,m3,m4,m5,m6,m7,m8" {
		t.Fatalf("messages = %s", got)
	}
	if peak < 2 || peak > 5 {
		t.Fatalf("peak concurrent reads = %d", peak)
	}
}
