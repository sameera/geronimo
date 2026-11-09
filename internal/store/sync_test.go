package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// openSyncStore opens a store with a token key, one account and one connected mailbox.
func openSyncStore(t *testing.T) (*Store, int64, int64) {
	t.Helper()
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "g.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.SetTokenKey(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	acct, err := st.UpsertAccount(ctx, "google", Claims{Subject: "sub-1", Email: "me@example.com", Name: "Me"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveMailbox(ctx, acct, Mailbox{Subject: "g-1", Email: "me@example.com", RefreshToken: "refresh-1", Scopes: []string{"scope"}}); err != nil {
		t.Fatal(err)
	}
	mb, err := st.MailboxID(ctx, acct, "g-1")
	if err != nil {
		t.Fatal(err)
	}
	return st, acct, mb
}

func TestEnsureSyncStartsRecordAndRestartsOnlyStoppedOnes(t *testing.T) { // D6
	st, _, mb := openSyncStore(t)
	ctx := context.Background()
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	rec, err := st.SyncFor(ctx, mb)
	if err != nil || rec.State != SyncStarting {
		t.Fatalf("new record = %+v, %v", rec, err)
	}
	if _, err := st.StoreMessages(ctx, mb, []Message{{ID: "m1", ThreadID: "t1", Sender: "a", Labels: []string{"INBOX"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.StopSync(ctx, mb, StopOther); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	rec, _ = st.SyncFor(ctx, mb)
	if rec.State != SyncStarting || rec.StopReason != "" {
		t.Fatalf("stopped record after EnsureSync = %+v", rec)
	}
	ids, _ := st.MessageIDs(ctx, mb)
	if len(ids) != 1 {
		t.Fatalf("stored mail lost on restart: %v", ids)
	}
	if err := st.StartRun(ctx, mb, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	rec, _ = st.SyncFor(ctx, mb)
	if rec.State != SyncDone {
		t.Fatalf("done record changed: %+v", rec)
	}
}

func TestStoreMessagesNeverStoresAnEmailTwice(t *testing.T) { // G7
	st, _, mb := openSyncStore(t)
	ctx := context.Background()
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	msg := Message{ID: "m1", ThreadID: "t1", Sender: "a", Subject: "s", Labels: []string{"INBOX"}}
	if n, err := st.StoreMessages(ctx, mb, []Message{msg}, nil); err != nil || n != 1 {
		t.Fatalf("first store = %d, %v", n, err)
	}
	if n, err := st.StoreMessages(ctx, mb, []Message{msg}, nil); err != nil || n != 0 {
		t.Fatalf("second store = %d, %v", n, err)
	}
	rec, _ := st.SyncFor(ctx, mb)
	if rec.SyncedCount != 1 {
		t.Fatalf("synced count = %d", rec.SyncedCount)
	}
}

func TestMessageRowsHoldMetadataOnly(t *testing.T) { // G12
	st, _, _ := openSyncStore(t)
	rows, err := st.db.Query(`SELECT name FROM pragma_table_info('message_records')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		cols[name] = true
	}
	for _, banned := range []string{"body", "snippet", "attachment", "content", "html", "text"} {
		if cols[banned] {
			t.Errorf("message_records holds %q", banned)
		}
	}
	for _, want := range []string{"sender", "subject", "date", "labels", "thread_id"} {
		if !cols[want] {
			t.Errorf("message_records lacks %q", want)
		}
	}
}

func TestRemovingAnAccountRemovesItsSyncData(t *testing.T) { // G19
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if _, err := st.StoreMessages(ctx, mb, []Message{{ID: "m1", Labels: []string{"INBOX"}}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.StoreListPage(ctx, mb, []string{"m2"}, "next", false); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM accounts WHERE id = ?`, acct); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"message_records", "pending_messages", "sync_records", "mailboxes"} {
		var n int
		if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s still holds %d rows", table, n)
		}
	}
}
