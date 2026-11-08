package gmail_test

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/sameera/geronimo/internal/gmail"
)

type recorder struct {
	mu   sync.Mutex
	hits []string
	auth []string
}

func newServer(t *testing.T) (*gmail.Client, *recorder) {
	t.Helper()
	rec := &recorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.mu.Lock()
		rec.hits = append(rec.hits, r.Method+" "+r.URL.RequestURI())
		rec.auth = append(rec.auth, r.Header.Get("Authorization"))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"emailAddress":"a@example.com","messagesTotal":7}`))
	}))
	t.Cleanup(srv.Close)
	return gmail.New(srv.URL+"/gmail/v1", srv.Client()), rec
}

func TestProfileUsesBearerTokenAndReturnsAccountData(t *testing.T) { // D7
	c, rec := newServer(t)
	p, err := c.Profile(context.Background(), "tok")
	if err != nil {
		t.Fatal(err)
	}
	if p.Email != "a@example.com" {
		t.Fatalf("profile = %+v", p)
	}
	if len(rec.hits) != 1 || rec.hits[0] != "GET /gmail/v1/users/me/profile" || rec.auth[0] != "Bearer tok" {
		t.Fatalf("hits = %v auth = %v", rec.hits, rec.auth)
	}
}

func TestAccessPointAllowsMetadataRequests(t *testing.T) { // G29
	c, rec := newServer(t)
	for _, tc := range []struct {
		path string
		q    url.Values
	}{
		{"/users/me/profile", nil},
		{"/users/me/labels", nil},
		{"/users/me/messages", url.Values{"maxResults": {"50"}}},
		{"/users/me/messages/abc123", url.Values{"format": {"metadata"}}},
		{"/users/me/messages/abc123", url.Values{"format": {"minimal"}}},
		{"/users/me/history", url.Values{"startHistoryId": {"1"}}},
	} {
		if _, err := c.Get(context.Background(), "tok", tc.path, tc.q); err != nil {
			t.Errorf("%s %v refused: %v", tc.path, tc.q, err)
		}
	}
	if len(rec.hits) != 6 {
		t.Fatalf("hits = %v", rec.hits)
	}
}

func TestAccessPointRefusesBodyBearingRequests(t *testing.T) { // G28, G29
	c, rec := newServer(t)
	for name, tc := range map[string]struct {
		path string
		q    url.Values
	}{
		"full format":              {"/users/me/messages/abc", url.Values{"format": {"full"}}},
		"raw format":               {"/users/me/messages/abc", url.Values{"format": {"raw"}}},
		"no format (default full)": {"/users/me/messages/abc", nil},
		"upper-case format":        {"/users/me/messages/abc", url.Values{"format": {"FULL"}}},
		"repeated format":          {"/users/me/messages/abc", url.Values{"format": {"metadata", "full"}}},
		"attachment":               {"/users/me/messages/abc/attachments/att1", nil},
		"thread (carries bodies)":  {"/users/me/threads/abc", url.Values{"format": {"metadata"}}},
		"drafts":                   {"/users/me/drafts", nil},
		"path traversal":           {"/users/me/messages/../drafts", nil},
		"encoded traversal":        {"/users/me/messages/%2e%2e/drafts", nil},
		"other user":               {"/users/other@example.com/profile", nil},
		"unknown":                  {"/users/me/settings/forwardingAddresses", nil},
		"format on list":           {"/users/me/messages", url.Values{"format": {"full"}}},
		"empty path":               {"", nil},
		"absolute url":             {"https://evil.example/users/me/profile", nil},
	} {
		if _, err := c.Get(context.Background(), "tok", tc.path, tc.q); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if len(rec.hits) != 0 {
		t.Fatalf("refused requests still reached Gmail: %v", rec.hits)
	}
}

func TestAccessPointErrorsNeverCarryTokensOrBodies(t *testing.T) { // G17, G36
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"secret-body tok"}`))
	}))
	defer srv.Close()
	c := gmail.New(srv.URL+"/gmail/v1", srv.Client())
	_, err := c.Profile(context.Background(), "tok")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "secret-body") || strings.Contains(err.Error(), "tok") {
		t.Fatalf("error leaks: %v", err)
	}
}

// TestNoGmailRequestBypassesTheAccessPoint fails when any non-test Go file outside
// internal/gmail names the Gmail API or its client library, or when the access point
// itself sends a request from more than one place. (G28, R1)
func TestNoGmailRequestBypassesTheAccessPoint(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{"gmail.googleapis.com", "googleapis.com/gmail", "google.golang.org/api/gmail", "/gmail/v1"}
	gmailDir := filepath.Join(root, "internal", "gmail")
	sends := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == ".nexus" || d.Name() == ".tmp") {
			return fs.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(b)
		if filepath.Dir(path) == gmailDir {
			sends += strings.Count(src, ".Do(")
			return nil
		}
		for _, f := range forbidden {
			if strings.Contains(src, f) {
				t.Errorf("%s names %q outside the Gmail access point", path, f)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if sends != 1 {
		t.Errorf("the access point sends requests from %d places, want exactly 1", sends)
	}
}
