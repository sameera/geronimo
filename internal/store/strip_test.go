package store

import (
	"context"
	"testing"
)

func TestStripDoneNoticeIsAckedOnce(t *testing.T) { // G15
	st, _, mb := openSyncStore(t)
	ctx := context.Background()
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, mb, 1); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	rec, _ := st.SyncFor(ctx, mb)
	if rec.StripAcked {
		t.Fatal("new done record is already acknowledged")
	}
	if err := st.MarkStripAcked(ctx, mb); err != nil {
		t.Fatal(err)
	}
	rec, _ = st.SyncFor(ctx, mb)
	if !rec.StripAcked {
		t.Fatal("ack not stored")
	}
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	rec, _ = st.SyncFor(ctx, mb)
	if rec.State != SyncDone || !rec.StripAcked {
		t.Fatalf("done record after EnsureSync = %+v", rec)
	}
}

func TestAccountSyncsListsEveryMailboxOfTheAccount(t *testing.T) {
	st, acct, _ := openSyncStore(t)
	ctx := context.Background()
	if _, err := st.SaveMailbox(ctx, acct, Mailbox{Subject: "g-2", Email: "two@example.com", RefreshToken: "r2", Scopes: []string{"s"}}); err != nil {
		t.Fatal(err)
	}
	mb2, err := st.MailboxID(ctx, acct, "g-2")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnsureSync(ctx, mb2); err != nil {
		t.Fatal(err)
	}
	recs, err := st.AccountSyncs(ctx, acct)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0].MailboxID != mb2 || recs[0].Email != "two@example.com" {
		t.Fatalf("syncs = %+v", recs)
	}
}
