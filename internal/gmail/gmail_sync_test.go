package gmail_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/sameera/geronimo/internal/gmail"
)

type uriLog struct {
	mu   sync.Mutex
	uris []string
}

func (l *uriLog) add(u string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.uris = append(l.uris, u)
}

func (l *uriLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.uris...)
}

// serveRecording answers every request with handler and records each request URI.
func serveRecording(t *testing.T, handler http.HandlerFunc) (*gmail.Client, *uriLog) {
	t.Helper()
	log := &uriLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.add(r.URL.RequestURI())
		handler(w, r)
	}))
	t.Cleanup(srv.Close)
	return gmail.New(srv.URL+"/gmail/v1", srv.Client()), log
}

func writeJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestAccessPointAllowsTheInboxLabelOnly(t *testing.T) { // D4
	c, log := serveRecording(t, writeJSON(`{"messagesTotal":7,"messagesUnread":3}`))
	if _, err := c.Get(context.Background(), "tok", "/users/me/labels/INBOX", nil); err != nil {
		t.Fatalf("inbox label refused: %v", err)
	}
	if got := log.all(); len(got) != 1 {
		t.Fatalf("hits = %v", got)
	}
	for name, tc := range map[string]struct {
		path string
		q    url.Values
	}{
		"other label":       {"/users/me/labels/Label_1", nil},
		"lower-case inbox":  {"/users/me/labels/inbox", nil},
		"inbox with format": {"/users/me/labels/INBOX", url.Values{"format": {"metadata"}}},
		"inbox sub-path":    {"/users/me/labels/INBOX/extra", nil},
	} {
		if _, err := c.Get(context.Background(), "tok", tc.path, tc.q); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if got := log.all(); len(got) != 1 {
		t.Fatalf("refused label requests reached Gmail: %v", got)
	}
}

func TestInboxCountsReadsTheInboxLabel(t *testing.T) { // D4
	c, log := serveRecording(t, writeJSON(`{"id":"INBOX","messagesTotal":7,"messagesUnread":3,"threadsTotal":2}`))
	got, err := c.InboxCounts(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 7 || got.Unread != 3 {
		t.Fatalf("counts = %+v", got)
	}
	if uris := log.all(); len(uris) != 1 || uris[0] != "/gmail/v1/users/me/labels/INBOX" {
		t.Fatalf("hits = %v", uris)
	}
}

func TestListInboxAsksForInboxIDsOnly(t *testing.T) { // D2
	c, log := serveRecording(t, writeJSON(`{"messages":[{"id":"a1","threadId":"t1"},{"id":"a2","threadId":"t2"}],"nextPageToken":"NEXT"}`))
	page, err := c.ListInbox(context.Background(), "tok", "PAGE")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(page.IDs, ",") != "a1,a2" || page.Next != "NEXT" {
		t.Fatalf("page = %+v", page)
	}
	uri := log.all()[0]
	for _, want := range []string{"labelIds=INBOX", "pageToken=PAGE"} {
		if !strings.Contains(uri, want) {
			t.Errorf("%q lacks %q", uri, want)
		}
	}
	if strings.Contains(uri, "format=") {
		t.Errorf("list request carries a format: %q", uri)
	}
}

func TestListInboxEmptyInboxHasNoNextPage(t *testing.T) {
	c, _ := serveRecording(t, writeJSON(`{"resultSizeEstimate":0}`))
	page, err := c.ListInbox(context.Background(), "tok", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.IDs) != 0 || page.Next != "" {
		t.Fatalf("page = %+v", page)
	}
}

func TestMetadataReadsHeadersAndLabelsOnly(t *testing.T) { // D3, G12
	c, log := serveRecording(t, writeJSON(`{"id":"a1","threadId":"t1","labelIds":["INBOX","UNREAD"],"snippet":"never stored","payload":{"headers":[{"name":"From","value":"x@example.com"},{"name":"Subject","value":"Hi"},{"name":"List-Id","value":"<list.example.com>"}]}}`))
	m, err := c.Metadata(context.Background(), "tok", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "a1" || m.ThreadID != "t1" || strings.Join(m.Labels, ",") != "INBOX,UNREAD" {
		t.Fatalf("message = %+v", m)
	}
	if m.From != "x@example.com" || m.Subject != "Hi" || m.ListID != "<list.example.com>" {
		t.Fatalf("headers = %+v", m)
	}
	uri := log.all()[0]
	if !strings.Contains(uri, "format=metadata") || strings.Contains(uri, "format=full") {
		t.Errorf("uri = %q", uri)
	}
	for _, h := range []string{"From", "To", "Subject", "Date", "List-Id", "List-Unsubscribe"} {
		if !strings.Contains(uri, "metadataHeaders="+h) {
			t.Errorf("uri %q lacks metadataHeaders=%s", uri, h)
		}
	}
}

func TestStatusErrorsCarryTheCodeOnly(t *testing.T) { // G21
	for _, tc := range []struct {
		code      int
		rateLimit bool
		notFound  bool
	}{
		{http.StatusTooManyRequests, true, false},
		{http.StatusNotFound, false, true},
		{http.StatusInternalServerError, false, false},
	} {
		c, _ := serveRecording(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte("secret body"))
		})
		_, err := c.Get(context.Background(), "tok", "/users/me/labels/INBOX", nil)
		if err == nil {
			t.Fatalf("code %d: no error", tc.code)
		}
		var se *gmail.StatusError
		if !errors.As(err, &se) || se.Code != tc.code {
			t.Fatalf("code %d: error = %v", tc.code, err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Fatalf("error leaks the body: %v", err)
		}
		if gmail.IsRateLimited(err) != tc.rateLimit || gmail.IsNotFound(err) != tc.notFound {
			t.Fatalf("code %d: rateLimited=%v notFound=%v", tc.code, gmail.IsRateLimited(err), gmail.IsNotFound(err))
		}
	}
	if gmail.IsRateLimited(nil) {
		t.Fatal("nil error reads as rate limited")
	}
}
