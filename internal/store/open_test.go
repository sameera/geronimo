package store

import (
	"context"
	"errors"
	"testing"
)

func TestSenderEmailsCarryTheirMailboxAddress(t *testing.T) { // #43
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	if _, err := st.StoreMessages(ctx, mb, []Message{msg("1", "a@x.com", "")}, nil); err != nil {
		t.Fatal(err)
	}
	emails, _, err := st.SenderEmails(ctx, acct, "a@x.com", nil, 10)
	if err != nil || len(emails) != 1 || emails[0].MailboxEmail != "me@example.com" {
		t.Fatalf("emails = %+v, %v", emails, err)
	}
}

func TestEmailForReadsOnlyTheAccountsOwnMail(t *testing.T) { // #43
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	if _, err := st.StoreMessages(ctx, mb, []Message{msg("1", "a@x.com", "", "INBOX", "UNREAD")}, nil); err != nil {
		t.Fatal(err)
	}
	e, sub, err := st.EmailFor(ctx, acct, mb, "1")
	if err != nil || sub != "g-1" || e.MessageID != "1" || !e.Unread || e.MailboxEmail != "me@example.com" || e.Subject != "s1" {
		t.Fatalf("email = %+v, %q, %v", e, sub, err)
	}
	other, err := st.UpsertAccount(ctx, "google", Claims{Subject: "sub-2", Email: "two@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.EmailFor(ctx, other, mb, "1"); !errors.Is(err, ErrNoEmail) {
		t.Errorf("foreign account read: %v", err)
	}
	if _, _, err := st.EmailFor(ctx, acct, mb, "missing"); !errors.Is(err, ErrNoEmail) {
		t.Errorf("missing email: %v", err)
	}
}

func TestMarkEmailReadClearsUnreadAndItsLabel(t *testing.T) { // #43, #44
	st, acct, mb := openSyncStore(t)
	ctx := context.Background()
	if _, err := st.StoreMessages(ctx, mb, []Message{
		msg("1", "a@x.com", "", "INBOX", "UNREAD"),
		msg("2", "a@x.com", "", "UNREAD"),
	}, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkEmailRead(ctx, mb, "1"); err != nil {
		t.Fatal(err)
	}
	groups, _, err := st.SenderGroups(ctx, acct, SortTotal, 10)
	if err != nil || len(groups) != 1 || groups[0].Unread != 1 || groups[0].Total != 2 {
		t.Fatalf("groups = %+v, %v", groups, err)
	}
	var labels string
	if err := st.db.QueryRow(`SELECT labels FROM message_records WHERE message_id = '1'`).Scan(&labels); err != nil || labels != "INBOX" {
		t.Fatalf("labels = %q, %v", labels, err)
	}
	if err := st.MarkEmailRead(ctx, mb, "1"); err != nil {
		t.Fatalf("second mark: %v", err)
	}
}
