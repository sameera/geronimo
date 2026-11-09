package auth_test

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
)

func TestConnectWhoseSyncCannotStartShowsRetry(t *testing.T) { // G1, #37 AC1
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	// Make the sync record insert fail after the mailbox is saved.
	db, err := sql.Open("sqlite", "file:"+e.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_sync BEFORE INSERT ON sync_records BEGIN SELECT RAISE(ABORT, 'no sync'); END`); err != nil {
		t.Fatal(err)
	}
	resp := e.connect(e.client, "g1", grant("g-sub", "box@example.com"))
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("callback = %d", resp.StatusCode)
	}
	page, body := e.get(e.client, location(resp))
	if page.StatusCode != 200 {
		t.Fatalf("home = %d", page.StatusCode)
	}
	if !strings.Contains(body, "could not start") || !strings.Contains(body, `id="retry"`) {
		t.Errorf("home shows no sync failure with a retry: %s", body)
	}
	if e.mailboxes() != 1 {
		t.Error("mailbox not stored")
	}
}
