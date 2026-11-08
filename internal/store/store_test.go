package store_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sameera/geronimo/internal/store"
)

type fixture struct {
	st   *store.Store
	path string
	now  time.Time
	key  []byte
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{path: filepath.Join(t.TempDir(), "g.db"), now: time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC), key: make([]byte, 32)}
	if _, err := rand.Read(f.key); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(f.path, func() time.Time { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetTokenKey(f.key); err != nil {
		t.Fatal(err)
	}
	f.st = st
	t.Cleanup(func() { st.Close() })
	return f
}

func (f *fixture) account(t *testing.T, sub string) int64 {
	t.Helper()
	id, err := f.st.UpsertAccount(context.Background(), "google", store.Claims{Subject: sub, Email: sub + "@example.com", Name: sub})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (f *fixture) dbBytes(t *testing.T) []byte {
	t.Helper()
	var all []byte
	for _, suffix := range []string{"", "-wal"} {
		b, err := os.ReadFile(f.path + suffix)
		if err == nil {
			all = append(all, b...)
		}
	}
	return all
}

var gmailBox = store.Mailbox{
	Subject: "g-sub-1", Email: "box@example.com", RefreshToken: "refresh-token-plaintext",
	Scopes: []string{"openid", "email", "https://www.googleapis.com/auth/gmail.modify"},
}

func TestSetTokenKeyRefusesWrongLength(t *testing.T) { // G16
	f := newFixture(t)
	if err := f.st.SetTokenKey([]byte("short")); err == nil {
		t.Fatal("short key accepted")
	}
}

func TestSaveMailboxRefusesWithoutKey(t *testing.T) { // G16
	st2, err := store.Open(filepath.Join(t.TempDir(), "other.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer st2.Close()
	id, _ := st2.UpsertAccount(context.Background(), "google", store.Claims{Subject: "x", Email: "x@e", Name: "x"})
	if _, err := st2.SaveMailbox(context.Background(), id, gmailBox); err == nil {
		t.Fatal("saved a token with no key set")
	}
}

func TestRefreshTokenIsStoredOnlyEncrypted(t *testing.T) { // G14
	f := newFixture(t)
	acct := f.account(t, "alice")
	if _, err := f.st.SaveMailbox(context.Background(), acct, gmailBox); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(f.dbBytes(t), []byte("refresh-token-plaintext")) {
		t.Fatal("database file contains the plaintext refresh token")
	}
	got, err := f.st.RefreshToken(context.Background(), acct, gmailBox.Subject)
	if err != nil || got != "refresh-token-plaintext" {
		t.Fatalf("refresh token = %q, %v", got, err)
	}
}

func TestDatabaseCopyWithoutKeyExposesNoToken(t *testing.T) { // G14
	f := newFixture(t)
	acct := f.account(t, "alice")
	if _, err := f.st.SaveMailbox(context.Background(), acct, gmailBox); err != nil {
		t.Fatal(err)
	}
	other := make([]byte, 32)
	_, _ = rand.Read(other)
	if err := f.st.SetTokenKey(other); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.RefreshToken(context.Background(), acct, gmailBox.Subject); !errors.Is(err, store.ErrUndecryptable) {
		t.Fatalf("err = %v, want ErrUndecryptable", err)
	}
}

func TestMailboxesListEmailAndStatus(t *testing.T) { // D10
	f := newFixture(t)
	acct := f.account(t, "alice")
	if got, _ := f.st.Mailboxes(context.Background(), acct); len(got) != 0 {
		t.Fatalf("mailboxes before connect = %v", got)
	}
	if _, err := f.st.SaveMailbox(context.Background(), acct, gmailBox); err != nil {
		t.Fatal(err)
	}
	got, err := f.st.Mailboxes(context.Background(), acct)
	if err != nil || len(got) != 1 || got[0].Email != "box@example.com" || got[0].Status != store.StatusConnected {
		t.Fatalf("mailboxes = %+v, %v", got, err)
	}
	if other := f.account(t, "bob"); other != acct {
		if got, _ := f.st.Mailboxes(context.Background(), other); len(got) != 0 {
			t.Fatalf("bob sees alice's mailbox: %v", got)
		}
	}
}

func TestConnectAttemptIsSingleUseBoundAndExpires(t *testing.T) { // G6, G8
	f := newFixture(t)
	ctx := context.Background()
	acct := f.account(t, "alice")
	mk := func(state string) {
		if err := f.st.CreateConnectAttempt(ctx, state, "nonce", "verifier", "browser-1", acct, 5*time.Minute); err != nil {
			t.Fatal(err)
		}
	}
	mk("s1")
	a, err := f.st.ConsumeConnectAttempt(ctx, "s1", "browser-1")
	if err != nil || a.AccountID != acct || a.Nonce != "nonce" || a.Verifier != "verifier" {
		t.Fatalf("attempt = %+v, %v", a, err)
	}
	if _, err := f.st.ConsumeConnectAttempt(ctx, "s1", "browser-1"); !errors.Is(err, store.ErrNoAttempt) {
		t.Errorf("reuse: %v", err)
	}
	mk("s2")
	if _, err := f.st.ConsumeConnectAttempt(ctx, "s2", "browser-2"); !errors.Is(err, store.ErrNoAttempt) {
		t.Errorf("wrong browser: %v", err)
	}
	mk("s3")
	f.now = f.now.Add(6 * time.Minute)
	if _, err := f.st.ConsumeConnectAttempt(ctx, "s3", "browser-1"); !errors.Is(err, store.ErrNoAttempt) {
		t.Errorf("expired: %v", err)
	}
	if _, err := f.st.ConsumeConnectAttempt(ctx, "missing", "browser-1"); !errors.Is(err, store.ErrNoAttempt) {
		t.Errorf("missing: %v", err)
	}
}

func TestSignInAttemptCannotFinishAConnect(t *testing.T) { // D3, D2
	f := newFixture(t)
	ctx := context.Background()
	if err := f.st.CreateAttempt(ctx, "signin-state", "n", "v", "browser-1", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.ConsumeConnectAttempt(ctx, "signin-state", "browser-1"); !errors.Is(err, store.ErrNoAttempt) {
		t.Fatalf("sign-in attempt consumed as connect attempt: %v", err)
	}
	acct := f.account(t, "alice")
	if err := f.st.CreateConnectAttempt(ctx, "connect-state", "n", "v", "browser-1", acct, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := f.st.ConsumeAttempt(ctx, "connect-state", "browser-1"); !errors.Is(err, store.ErrNoAttempt) {
		t.Fatalf("connect attempt consumed as sign-in attempt: %v", err)
	}
}

func TestPurgeRemovesExpiredConnectAttempts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	acct := f.account(t, "alice")
	_ = f.st.CreateConnectAttempt(ctx, "s", "n", "v", "b", acct, time.Minute)
	f.now = f.now.Add(time.Hour)
	if err := f.st.PurgeExpired(ctx); err != nil {
		t.Fatal(err)
	}
	n, err := f.st.CountConnectAttempts(ctx)
	if err != nil || n != 0 {
		t.Fatalf("connect attempts after purge = %d, %v", n, err)
	}
}

func TestSaveMailboxTwiceReplacesInPlace(t *testing.T) { // G11, D5
	f := newFixture(t)
	ctx := context.Background()
	acct := f.account(t, "alice")
	out, err := f.st.SaveMailbox(ctx, acct, gmailBox)
	if err != nil || out != store.Added {
		t.Fatalf("first save = %v, %v", out, err)
	}
	again := gmailBox
	again.Email = "renamed@example.com"
	again.RefreshToken = "second-refresh-token"
	again.Scopes = []string{"openid", "email"}
	out, err = f.st.SaveMailbox(ctx, acct, again)
	if err != nil || out != store.Replaced {
		t.Fatalf("second save = %v, %v", out, err)
	}
	got, _ := f.st.Mailboxes(ctx, acct)
	if len(got) != 1 || got[0].Email != "renamed@example.com" || got[0].Status != store.StatusConnected {
		t.Fatalf("mailboxes = %+v", got)
	}
	tok, err := f.st.RefreshToken(ctx, acct, gmailBox.Subject)
	if err != nil || tok != "second-refresh-token" {
		t.Fatalf("token = %q, %v", tok, err)
	}
	if bytes.Contains(f.dbBytes(t), []byte("second-refresh-token")) {
		t.Error("replacement token stored in plain text")
	}
}

func TestSameMailboxUnderTwoAccountsKeepsEachGrant(t *testing.T) { // D5
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.account(t, "alice"), f.account(t, "bob")
	one, two := gmailBox, gmailBox
	one.RefreshToken, two.RefreshToken = "alice-token", "bob-token"
	if _, err := f.st.SaveMailbox(ctx, a, one); err != nil {
		t.Fatal(err)
	}
	if out, err := f.st.SaveMailbox(ctx, b, two); err != nil || out != store.Added {
		t.Fatalf("bob's save = %v, %v", out, err)
	}
	ta, _ := f.st.RefreshToken(ctx, a, gmailBox.Subject)
	tb, _ := f.st.RefreshToken(ctx, b, gmailBox.Subject)
	if ta != "alice-token" || tb != "bob-token" {
		t.Fatalf("tokens = %q %q", ta, tb)
	}
}

func TestDifferentSubjectsAreDifferentMailboxes(t *testing.T) { // G13
	f := newFixture(t)
	ctx := context.Background()
	acct := f.account(t, "alice")
	second := gmailBox
	second.Subject, second.Email = "g-sub-2", "second@example.com"
	for _, m := range []store.Mailbox{gmailBox, second} {
		if _, err := f.st.SaveMailbox(ctx, acct, m); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := f.st.Mailboxes(ctx, acct)
	if len(got) != 2 {
		t.Fatalf("mailboxes = %+v", got)
	}
}

func TestAccessRemovedStatusAndReconnect(t *testing.T) { // G18, D5
	f := newFixture(t)
	ctx := context.Background()
	acct := f.account(t, "alice")
	_, _ = f.st.SaveMailbox(ctx, acct, gmailBox)
	if err := f.st.MarkAccessRemoved(ctx, acct, gmailBox.Subject); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.Mailboxes(ctx, acct)
	if got[0].Status != store.StatusRemoved {
		t.Fatalf("status = %q", got[0].Status)
	}
	out, err := f.st.SaveMailbox(ctx, acct, gmailBox)
	if err != nil || out != store.Reconnected {
		t.Fatalf("save after removal = %v, %v", out, err)
	}
	got, _ = f.st.Mailboxes(ctx, acct)
	if got[0].Status != store.StatusConnected {
		t.Fatalf("status after reconnect = %q", got[0].Status)
	}
}

func TestClaimDueChecksIsAtomicAndHonoursInterval(t *testing.T) { // D8
	f := newFixture(t)
	ctx := context.Background()
	acct := f.account(t, "alice")
	_, _ = f.st.SaveMailbox(ctx, acct, gmailBox)
	removed := gmailBox
	removed.Subject = "g-sub-2"
	_, _ = f.st.SaveMailbox(ctx, acct, removed)
	_ = f.st.MarkAccessRemoved(ctx, acct, "g-sub-2")

	if got, _ := f.st.ClaimDueChecks(ctx, acct, 15*time.Minute); len(got) != 0 {
		t.Fatalf("claimed before the interval: %v", got)
	}
	f.now = f.now.Add(16 * time.Minute)
	got, err := f.st.ClaimDueChecks(ctx, acct, 15*time.Minute)
	if err != nil || len(got) != 1 || got[0] != gmailBox.Subject {
		t.Fatalf("claimed = %v, %v (removed mailboxes need no check)", got, err)
	}
	if again, _ := f.st.ClaimDueChecks(ctx, acct, 15*time.Minute); len(again) != 0 {
		t.Fatalf("claimed twice: %v", again)
	}
	f.now = f.now.Add(16 * time.Minute)
	if again, _ := f.st.ClaimDueChecks(ctx, acct, 15*time.Minute); len(again) != 1 {
		t.Fatalf("not claimed after the next interval: %v", again)
	}
}
