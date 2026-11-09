package mailsync_test

import (
	"context"
	"testing"

	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
)

// setCounts changes the inbox counts Gmail reports for a token.
func (f *fakeGmail) setCounts(token string, c gmail.Counts) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counts[token] = c
}

func (e *env) baseline(id int64) (store.Baseline, bool) {
	e.t.Helper()
	b, ok, err := e.st.Baseline(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return b, ok
}

func TestBaselineIsInboxUnreadWhenTheSyncFinishes(t *testing.T) { // G16
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	token := "access-refresh-a"
	e.gm.setInbox(token, []string{"m1"})
	e.gm.setCounts(token, gmail.Counts{Total: 10, Unread: 4})
	e.gm.hook = func(op, tok string, page int) error {
		if op == "list" && page == 0 {
			// Mail is read while the sync runs, so the unread count moves before the finish.
			e.gm.setCounts(token, gmail.Counts{Total: 12, Unread: 2})
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if rec := e.sync(id); rec.State != store.SyncDone {
		t.Fatalf("state = %s / %q", rec.State, rec.StopReason)
	}
	b, ok := e.baseline(id)
	if !ok || b.Unread != 2 {
		t.Fatalf("baseline = %+v ok=%v, want unread 2 read at the finish", b, ok)
	}
}

func TestBaselineWaitsForTheFinish(t *testing.T) { // G17
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	token := "access-refresh-a"
	e.gm.setInbox(token, []string{"m2", "m1"}, []string{"m0"})
	e.gm.setCounts(token, gmail.Counts{Total: 3, Unread: 3})
	release := make(chan struct{})
	e.gm.hook = func(op, tok string, page int) error {
		if op == "list" && page == 1 {
			<-release
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	waitFor(t, "first batch", func() bool { return len(e.messages(id)) == 2 })
	if _, ok := e.baseline(id); ok {
		t.Fatal("baseline written before the sync finished")
	}
	close(release)
	w.Wait()
	if b, ok := e.baseline(id); !ok || b.Unread != 3 {
		t.Fatalf("baseline after finish = %+v ok=%v", b, ok)
	}
}

func TestRetriedFirstSyncWritesOneBaseline(t *testing.T) { // G17, G18
	e := newEnv(t)
	id := e.mailbox("a", "refresh-a")
	token := "access-refresh-a"
	e.gm.setInbox(token, []string{"m2", "m1"}, []string{"m0"})
	e.gm.setCounts(token, gmail.Counts{Total: 3, Unread: 3})
	broken := true
	e.gm.hook = func(op, tok string, page int) error {
		if op == "list" && page == 1 && broken {
			return serverErr
		}
		return nil
	}
	w := e.worker()
	e.start(w, id)
	w.Wait()
	if _, ok := e.baseline(id); ok {
		t.Fatal("stopped sync wrote a baseline")
	}
	broken = false
	e.gm.setCounts(token, gmail.Counts{Total: 3, Unread: 1})
	if err := e.st.EnsureSync(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	w2 := e.worker()
	w2.Kick(id)
	w2.Wait()
	if b, ok := e.baseline(id); !ok || b.Unread != 1 {
		t.Fatalf("baseline after retry = %+v ok=%v", b, ok)
	}
	// A reconnect after the finish leaves the baseline alone.
	e.gm.setCounts(token, gmail.Counts{Total: 3, Unread: 0})
	if err := e.st.EnsureSync(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	w3 := e.worker()
	w3.Kick(id)
	w3.Wait()
	if b, _ := e.baseline(id); b.Unread != 1 {
		t.Fatalf("baseline changed after reconnect: %+v", b)
	}
}
