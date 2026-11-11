package gmail_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sameera/geronimo/internal/gmail"
)

type writeRecorder struct {
	mu     sync.Mutex
	hits   []string
	bodies []string
	auth   []string
	ctype  []string
}

func newWriteServer(t *testing.T, status int) (*gmail.Client, *writeRecorder) {
	t.Helper()
	rec := &writeRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.hits = append(rec.hits, r.Method+" "+r.URL.RequestURI())
		rec.bodies = append(rec.bodies, string(b))
		rec.auth = append(rec.auth, r.Header.Get("Authorization"))
		rec.ctype = append(rec.ctype, r.Header.Get("Content-Type"))
		rec.mu.Unlock()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"id":"abc","labelIds":["INBOX"]}`))
	}))
	t.Cleanup(srv.Close)
	return gmail.New(srv.URL+"/gmail/v1", srv.Client()), rec
}

func TestMarkReadRemovesOnlyTheUnreadLabel(t *testing.T) { // #43
	c, rec := newWriteServer(t, http.StatusOK)
	if err := c.MarkRead(context.Background(), "tok", "abc123"); err != nil {
		t.Fatal(err)
	}
	if len(rec.hits) != 1 || rec.hits[0] != "POST /gmail/v1/users/me/messages/abc123/modify" {
		t.Fatalf("hits = %v", rec.hits)
	}
	if rec.bodies[0] != `{"removeLabelIds":["UNREAD"]}` {
		t.Errorf("body = %s", rec.bodies[0])
	}
	if rec.auth[0] != "Bearer tok" || !strings.HasPrefix(rec.ctype[0], "application/json") {
		t.Errorf("auth = %q content type = %q", rec.auth[0], rec.ctype[0])
	}
}

func TestMarkReadRefusesBadMessageIDs(t *testing.T) { // #43, G28
	c, rec := newWriteServer(t, http.StatusOK)
	for _, id := range []string{"", "a/b", "..", "a?b", "a#b", "a%2fb", "a\\b", "abc/modify"} {
		if err := c.MarkRead(context.Background(), "tok", id); err == nil {
			t.Errorf("id %q accepted", id)
		}
	}
	if len(rec.hits) != 0 {
		t.Fatalf("refused requests reached Gmail: %v", rec.hits)
	}
}

func TestMarkReadReportsGmailRefusal(t *testing.T) { // #43
	c, _ := newWriteServer(t, http.StatusInternalServerError)
	err := c.MarkRead(context.Background(), "tok", "abc")
	var se *gmail.StatusError
	if err == nil || !errors.As(err, &se) || se.Code != http.StatusInternalServerError {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "tok") || strings.Contains(err.Error(), "labelIds") {
		t.Fatalf("error leaks: %v", err)
	}
}

func TestGetStillRefusesTheModifyPath(t *testing.T) { // G28: the write is MarkRead only
	c, rec := newWriteServer(t, http.StatusOK)
	if _, err := c.Get(context.Background(), "tok", "/users/me/messages/abc/modify", nil); err == nil {
		t.Error("GET on modify accepted")
	}
	if len(rec.hits) != 0 {
		t.Fatalf("hits = %v", rec.hits)
	}
}

func TestWebURLOpensTheMessageInItsOwnAccount(t *testing.T) { // #43
	got := gmail.WebURL("two@example.com", "18c0ffee")
	want := "https://mail.google.com/mail/?authuser=two%40example.com#all/18c0ffee"
	if got != want {
		t.Fatalf("WebURL = %q, want %q", got, want)
	}
}
