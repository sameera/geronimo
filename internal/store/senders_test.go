package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func msg(id, from, date string, labels ...string) Message {
	return Message{ID: id, ThreadID: "t" + id, Sender: from, Subject: "s" + id, Date: date, Labels: labels}
}

func TestDeriveSenderKeyAndName(t *testing.T) { // D1, D2, G1
	cases := []struct {
		from, key, name string
	}{
		{`Ann Lee <Ann@Example.COM>`, "ann@example.com", "Ann Lee"},
		{`ann@example.com`, "ann@example.com", ""},
		{`"News" <news+a@x.com>`, "news+a@x.com", "News"},
		{`=?UTF-8?Q?Caf=C3=A9?= <cafe@x.com>`, "cafe@x.com", "Café"},
		{`  Weird Header  `, "weird header", ""},
		{``, UnknownSenderKey, ""},
	}
	for _, c := range cases {
		d := Derive(c.from, "", nil)
		if d.Key != c.key || d.Name != c.name {
			t.Errorf("Derive(%q) = %q, %q; want %q, %q", c.from, d.Key, d.Name, c.key, c.name)
		}
	}
}

func TestDeriveDateAndUnread(t *testing.T) { // D3, G5, G7
	d := Derive("a@x.com", "Mon, 02 Jan 2006 15:04:05 -0700", []string{"INBOX", "UNREAD"})
	if want := time.Date(2006, 1, 2, 22, 4, 5, 0, time.UTC).Unix(); d.Date != want || !d.Unread {
		t.Fatalf("derived = %+v, want date %d unread", d, want)
	}
	for _, bad := range []string{"", "not a date"} {
		if d := Derive("a@x.com", bad, []string{"INBOX"}); d.Date != UnknownDate || d.Unread {
			t.Errorf("Derive date %q = %+v", bad, d)
		}
	}
}

func TestGroupsCountEveryEmailOnce(t *testing.T) { // G1, G2, G5, G6, G7
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	_, err := st.StoreMessages(ctx, mb, []Message{
		msg("1", "Old Name <a@x.com>", "Mon, 02 Jan 2006 15:04:05 +0000", "UNREAD"),
		msg("2", "New Name <A@X.com>", "Tue, 03 Jan 2006 15:04:05 +0000"),
		msg("3", "b@x.com", "garbage"),
		msg("4", "", ""),
		msg("5", "weird", "Wed, 04 Jan 2006 15:04:05 +0000", "UNREAD"),
		msg("6", "a+tag@x.com", "Wed, 04 Jan 2006 15:04:05 +0000"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	groups, total, err := st.SenderGroups(ctx, acct, SortTotal, 100)
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	by := map[string]SenderGroup{}
	for _, g := range groups {
		sum += g.Total
		by[g.Key] = g
	}
	if sum != 6 || total != len(groups) || len(groups) != 5 {
		t.Fatalf("sum=%d total=%d groups=%+v", sum, total, groups)
	}
	a := by["a@x.com"]
	if a.Total != 2 || a.Unread != 1 || a.Name != "New Name" || a.Address != "a@x.com" {
		t.Errorf("a group = %+v", a)
	}
	if want := time.Date(2006, 1, 3, 15, 4, 5, 0, time.UTC).Unix(); a.Newest != want {
		t.Errorf("newest = %d", a.Newest)
	}
	if g := by["b@x.com"]; g.Newest != UnknownDate || g.Name != "" {
		t.Errorf("unparseable date became newest: %+v", g)
	}
	if g := by[UnknownSenderKey]; g.Total != 1 || g.Address != "" {
		t.Errorf("unknown group = %+v", g)
	}
	if groups[0].Key != "a@x.com" {
		t.Errorf("largest group first, got %q", groups[0].Key)
	}
	// ties by address
	if groups[1].Key > groups[2].Key {
		t.Errorf("ties not ordered by address: %q %q", groups[1].Key, groups[2].Key)
	}
}

func TestGroupsMergeMailboxesAndStayPerAccount(t *testing.T) { // G9, G10
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	if _, err := st.SaveMailbox(ctx, acct, Mailbox{Subject: "g-2", Email: "two@example.com", RefreshToken: "r2", Scopes: []string{"s"}}); err != nil {
		t.Fatal(err)
	}
	mb2, _ := st.MailboxID(ctx, acct, "g-2")
	if _, err := st.db.Exec(`UPDATE mailboxes SET status = ? WHERE id = ?`, StatusRemoved, mb2); err != nil {
		t.Fatal(err)
	}
	other, _ := st.UpsertAccount(ctx, "google", Claims{Subject: "sub-2", Email: "o@example.com", Name: "O"})
	if _, err := st.SaveMailbox(ctx, other, Mailbox{Subject: "g-3", Email: "o@example.com", RefreshToken: "r3", Scopes: []string{"s"}}); err != nil {
		t.Fatal(err)
	}
	mb3, _ := st.MailboxID(ctx, other, "g-3")
	st.StoreMessages(ctx, mb, []Message{msg("1", "a@x.com", "")}, nil)
	st.StoreMessages(ctx, mb2, []Message{msg("1", "a@x.com", "")}, nil)
	st.StoreMessages(ctx, mb3, []Message{msg("1", "a@x.com", ""), msg("2", "secret@y.com", "")}, nil)
	groups, _, err := st.SenderGroups(ctx, acct, SortTotal, 100)
	if err != nil || len(groups) != 1 || groups[0].Total != 2 {
		t.Fatalf("groups = %+v, %v", groups, err)
	}
}

func TestGroupsLimit(t *testing.T) { // D5
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	st.StoreMessages(ctx, mb, []Message{msg("1", "a@x.com", ""), msg("2", "b@x.com", ""), msg("3", "c@x.com", "")}, nil)
	groups, total, err := st.SenderGroups(ctx, acct, SortTotal, 2)
	if err != nil || len(groups) != 2 || total != 3 {
		t.Fatalf("groups = %+v total=%d err=%v", groups, total, err)
	}
}

func TestOpenBackfillsMailStoredByEpic4(t *testing.T) { // R1, G4, G26
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + syncSchema); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO accounts (id, created_at) VALUES (1, 0)`)
	mustExec(`INSERT INTO mailboxes (id, account_id, google_subject, email, refresh_token_enc, scopes, status, last_checked_at) VALUES (1, 1, 'g', 'me@x.com', x'00', 's', 'connected', 0)`)
	ins := `INSERT INTO message_records (mailbox_id, message_id, thread_id, sender, recipients, subject, date, list_id, list_unsubscribe, labels) VALUES (1, ?, 't', ?, '', 's', ?, '', '', ?)`
	mustExec(ins, "1", "Ann <ANN@x.com>", "Mon, 02 Jan 2006 15:04:05 +0000", "INBOX,UNREAD")
	mustExec(ins, "2", "ann@x.com", "", "INBOX")
	db.Close()

	for i := 0; i < 2; i++ { // the step is idempotent
		st, err := Open(path, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		groups, _, err := st.SenderGroups(context.Background(), 1, SortTotal, 100)
		if err != nil || len(groups) != 1 || groups[0].Total != 2 || groups[0].Unread != 1 || groups[0].Name != "Ann" {
			t.Fatalf("open %d: groups = %+v, %v", i, groups, err)
		}
		st.Close()
	}
}

func TestStoreMessagesKeepsNoBodyColumns(t *testing.T) { // G24
	st, _, _ := openSyncStore(t)
	rows, err := st.db.Query(`SELECT name FROM pragma_table_info('message_records')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		rows.Scan(&n)
		if n == "body" || n == "snippet" {
			t.Errorf("column %q stores content", n)
		}
	}
}

func TestEmailsPageThroughEverySenderEmailOnce(t *testing.T) { // G3, G19, D8
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	var msgs []Message
	for i := 1; i <= 7; i++ {
		msgs = append(msgs, msg(string(rune('a'+i)), "a@x.com", "Mon, 0"+string(rune('0'+i))+" Jan 2006 10:00:00 +0000"))
	}
	msgs = append(msgs, msg("u1", "A@x.com", "bad"), msg("u2", "a@x.com", ""), msg("z", "other@x.com", ""))
	st.StoreMessages(ctx, mb, msgs, nil)

	sum, err := st.SenderSummary(ctx, acct, "a@x.com")
	if err != nil || sum.Total != 9 || sum.Address != "a@x.com" {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
	seen := map[string]bool{}
	var after *EmailCursor
	var order []int64
	for pages := 0; pages < 10; pages++ {
		page, next, err := st.SenderEmails(ctx, acct, "a@x.com", after, 4)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range page {
			if seen[e.MessageID] {
				t.Fatalf("%s repeated", e.MessageID)
			}
			seen[e.MessageID] = true
			order = append(order, e.Date)
		}
		if pages == 0 {
			// a sync stores older and newer mail between pages: nothing repeats or is skipped
			st.StoreMessages(ctx, mb, []Message{msg("new", "a@x.com", "Fri, 01 Jan 2010 10:00:00 +0000"), msg("old", "a@x.com", "Sun, 01 Jan 1995 10:00:00 +0000")}, nil)
		}
		if next == nil {
			break
		}
		after = next
	}
	for _, id := range []string{"b", "c", "d", "e", "f", "g", "h", "u1", "u2", "old"} {
		if !seen[id] {
			t.Errorf("%s was skipped", id)
		}
	}
	if seen["z"] {
		t.Error("another sender's email listed")
	}
	// newest first; unknown dates last
	for i := 1; i < len(order); i++ {
		if order[i] > order[i-1] {
			t.Fatalf("order not newest first: %v", order)
		}
	}
}

func TestEmailsAreScopedToAccountAndUnknownSenderIsEmpty(t *testing.T) { // G10
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	other, _ := st.UpsertAccount(ctx, "google", Claims{Subject: "sub-2", Email: "o@example.com", Name: "O"})
	st.SaveMailbox(ctx, other, Mailbox{Subject: "g-3", Email: "o@example.com", RefreshToken: "r3", Scopes: []string{"s"}})
	mb3, _ := st.MailboxID(ctx, other, "g-3")
	st.StoreMessages(ctx, mb3, []Message{msg("1", "secret@y.com", "")}, nil)
	st.StoreMessages(ctx, mb, []Message{msg("1", "a@x.com", "")}, nil)
	page, next, err := st.SenderEmails(ctx, acct, "secret@y.com", nil, 50)
	if err != nil || len(page) != 0 || next != nil {
		t.Fatalf("page = %+v next=%v err=%v", page, next, err)
	}
	sum, err := st.SenderSummary(ctx, acct, "secret@y.com")
	if err != nil || sum.Total != 0 {
		t.Fatalf("summary = %+v, %v", sum, err)
	}
}
