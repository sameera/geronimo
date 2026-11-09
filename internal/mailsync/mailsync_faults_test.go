package mailsync_test

import (
	"context"
	"testing"

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
