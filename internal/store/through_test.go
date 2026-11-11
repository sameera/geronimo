package store

import (
	"context"
	"testing"
)

func TestSenderEmailsThroughReReadsTheShownRows(t *testing.T) { // #44
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	if _, err := st.StoreMessages(ctx, mb, []Message{
		msg("a", "s@x.com", "Mon, 02 Jan 2006 10:00:00 +0000"),
		msg("b", "s@x.com", "Mon, 02 Jan 2006 10:00:00 +0000"),
		msg("c", "s@x.com", "Sun, 01 Jan 2006 10:00:00 +0000"),
		msg("d", "s@x.com", ""),
		msg("z", "other@x.com", ""),
	}, nil); err != nil {
		t.Fatal(err)
	}
	all, _, err := st.SenderEmails(ctx, acct, "s@x.com", nil, 10)
	if err != nil || len(all) != 4 {
		t.Fatalf("all = %+v, %v", all, err)
	}
	ids := func(es []Email) string {
		s := ""
		for _, e := range es {
			s += e.MessageID
		}
		return s
	}
	for i, want := range []string{"a", "ab", "abc", "abcd"} {
		cur := EmailCursor{Date: all[i].Date, MailboxID: all[i].MailboxID, MessageID: all[i].MessageID}
		got, err := st.SenderEmailsThrough(ctx, acct, "s@x.com", cur, 100)
		if err != nil || ids(got) != want {
			t.Errorf("through %s = %q, %v; want %q", all[i].MessageID, ids(got), err, want)
		}
	}
	cur := EmailCursor{Date: all[3].Date, MailboxID: all[3].MailboxID, MessageID: all[3].MessageID}
	if got, _ := st.SenderEmailsThrough(ctx, acct, "s@x.com", cur, 2); ids(got) != "ab" {
		t.Errorf("capped = %q", ids(got))
	}
	if got, _ := st.SenderEmailsThrough(ctx, acct+1, "s@x.com", cur, 100); len(got) != 0 {
		t.Errorf("foreign account read %d rows", len(got))
	}
}

func TestSenderSummaryCountsUnread(t *testing.T) { // #44
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	if _, err := st.StoreMessages(ctx, mb, []Message{
		msg("1", "s@x.com", "", "UNREAD"), msg("2", "s@x.com", "", "UNREAD"), msg("3", "s@x.com", ""),
	}, nil); err != nil {
		t.Fatal(err)
	}
	sum, err := st.SenderSummary(ctx, acct, "s@x.com")
	if err != nil || sum.Total != 3 || sum.Unread != 2 {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
	if err := st.MarkEmailRead(ctx, mb, "1"); err != nil {
		t.Fatal(err)
	}
	if sum, _ = st.SenderSummary(ctx, acct, "s@x.com"); sum.Unread != 1 {
		t.Fatalf("unread after mark = %d", sum.Unread)
	}
}
