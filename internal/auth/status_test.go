package auth_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sameera/geronimo/internal/auth"
	"github.com/sameera/geronimo/internal/store"
)

// fakeChecker stands in for Google's token endpoint. Results are keyed by refresh token.
type fakeChecker struct {
	mu      sync.Mutex
	results map[string]error
	calls   []string
	block   bool // wait for the request context to end, like a hung connection
}

func (c *fakeChecker) Check(ctx context.Context, refreshToken string) error {
	c.mu.Lock()
	c.calls = append(c.calls, refreshToken)
	err, block := c.results[refreshToken], c.block
	c.mu.Unlock()
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	return err
}

func (c *fakeChecker) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (e *env) setCheck(refreshToken string, err error) {
	e.chk.mu.Lock()
	defer e.chk.mu.Unlock()
	e.chk.results[refreshToken] = err
}

const refreshOne = "refresh-plaintext-g-sub-1"

func connectedEnv(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.connect(e.client, "g1", grant("g-sub-1", "one@example.com"))
	return e
}

func TestNoCheckRightAfterConnecting(t *testing.T) { // D8
	e := connectedEnv(t)
	e.get(e.client, "/")
	if e.chk.callCount() != 0 {
		t.Fatalf("checks = %d", e.chk.callCount())
	}
}

func TestStatusCheckRunsAtMostOncePerInterval(t *testing.T) { // D8
	e := connectedEnv(t)
	e.advance(16 * time.Minute)
	_, body := e.get(e.client, "/")
	if e.chk.callCount() != 1 || e.chk.calls[0] != refreshOne {
		t.Fatalf("checks = %v", e.chk.calls)
	}
	if !strings.Contains(body, "one@example.com: Connected") {
		t.Errorf("a working token changed the status: %s", body)
	}
	e.advance(5 * time.Minute)
	e.get(e.client, "/")
	e.get(e.client, "/")
	if e.chk.callCount() != 1 {
		t.Errorf("checks within the interval = %d, want 1", e.chk.callCount())
	}
	e.advance(11 * time.Minute)
	e.get(e.client, "/")
	if e.chk.callCount() != 2 {
		t.Errorf("checks after the interval = %d, want 2", e.chk.callCount())
	}
}

func TestRevokedAccessShowsRemovedMessageAndReconnect(t *testing.T) { // G19, D9, D10
	e := connectedEnv(t)
	e.connect(e.client, "g2", grant("g-sub-2", "two@example.com"))
	e.advance(16 * time.Minute)
	e.setCheck(refreshOne, auth.ErrAccessRemoved)
	page, body := e.get(e.client, "/")
	if page.StatusCode != http.StatusOK {
		t.Fatalf("home = %d", page.StatusCode)
	}
	li := regexp.MustCompile(`(?s)<li>one@example.com.*?</li>`).FindString(body)
	if !strings.Contains(li, "Access was removed") || !regexp.MustCompile(`<a[^>]*href="/connect"[^>]*>Reconnect</a>`).MatchString(li) {
		t.Errorf("removed account entry = %q", li)
	}
	if !strings.Contains(body, "two@example.com: Connected") {
		t.Errorf("the other account is not listed as connected: %s", body)
	}
	boxes, _ := e.st.Mailboxes(context.Background(), 1)
	if boxes[0].Status != store.StatusRemoved || boxes[1].Status != store.StatusConnected {
		t.Errorf("statuses = %+v", boxes)
	}
	// The status is stored, so it shows again without another Google call.
	calls := e.chk.callCount()
	_, body = e.get(e.client, "/")
	if !strings.Contains(body, "Access was removed") || e.chk.callCount() != calls {
		t.Errorf("removed status not kept (calls %d -> %d)", calls, e.chk.callCount())
	}
}

func TestTransientTroubleNeverMarksAccessRemoved(t *testing.T) { // G18
	for name, err := range map[string]error{
		"timeout":      context.DeadlineExceeded,
		"network":      errors.New("dial tcp: connection refused"),
		"server error": errors.New("provider returned status 503"),
	} {
		t.Run(name, func(t *testing.T) {
			e := connectedEnv(t)
			e.advance(16 * time.Minute)
			e.setCheck(refreshOne, err)
			page, body := e.get(e.client, "/")
			if page.StatusCode != http.StatusOK || !strings.Contains(body, "one@example.com: Connected") {
				t.Fatalf("home = %d %s", page.StatusCode, body)
			}
			boxes, _ := e.st.Mailboxes(context.Background(), 1)
			if boxes[0].Status != store.StatusConnected {
				t.Fatalf("status = %q", boxes[0].Status)
			}
		})
	}
}

func TestUndecryptableTokenMarksAccessRemovedWithoutAnErrorPage(t *testing.T) { // G18, D6
	e := connectedEnv(t)
	if err := e.st.SetTokenKey(bytesOf(9)); err != nil { // the key was replaced
		t.Fatal(err)
	}
	e.advance(16 * time.Minute)
	page, body := e.get(e.client, "/")
	if page.StatusCode != http.StatusOK || !strings.Contains(body, "Access was removed") {
		t.Fatalf("home = %d %s", page.StatusCode, body)
	}
	if e.chk.callCount() != 0 {
		t.Error("Google was called with an unreadable token")
	}
}

func TestSlowStatusCheckNeverDelaysHomeMoreThanAboutThreeSeconds(t *testing.T) { // G22
	e := connectedEnv(t)
	e.chk.block = true
	e.advance(16 * time.Minute)
	start := time.Now()
	page, body := e.get(e.client, "/")
	elapsed := time.Since(start)
	if page.StatusCode != http.StatusOK || !strings.Contains(body, "one@example.com: Connected") {
		t.Fatalf("home = %d %s", page.StatusCode, body)
	}
	if elapsed > 4*time.Second {
		t.Errorf("home took %v", elapsed)
	}
	boxes, _ := e.st.Mailboxes(context.Background(), 1)
	if boxes[0].Status != store.StatusConnected {
		t.Errorf("a timeout marked access removed")
	}
}

func TestStatusChecksNeverLogTokens(t *testing.T) { // G17
	e := connectedEnv(t)
	e.advance(16 * time.Minute)
	e.setCheck(refreshOne, errors.New("provider returned status 503"))
	e.get(e.client, "/")
	if strings.Contains(e.logs.String(), "refresh-plaintext") || strings.Contains(e.logs.String(), "access-plaintext") {
		t.Errorf("logs carry a token: %s", e.logs.String())
	}
}

func TestRetryAndReconnectLeadToPreConsentScreen(t *testing.T) { // G24
	e := connectedEnv(t)
	e.advance(16 * time.Minute)
	e.setCheck(refreshOne, auth.ErrAccessRemoved)
	_, body := e.get(e.client, "/")
	if !strings.Contains(body, `href="/connect"`) {
		t.Fatalf("no reconnect link: %s", body)
	}
	page, body := e.get(e.client, "/connect")
	if page.StatusCode != http.StatusOK || !strings.Contains(body, `id="pre-consent"`) || !strings.Contains(body, `id="connect-gmail"`) {
		t.Fatalf("/connect = %d %s", page.StatusCode, body)
	}
	if page.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", page.Header.Get("Cache-Control"))
	}

	// A denial for a user who already has an account connected also retries through it.
	e2 := connectedEnv(t)
	state := stateOf(t, e2.connectStart(e2.client))
	resp := e2.callback(e2.client, "error=access_denied&state="+url.QueryEscape(state))
	_, body = e2.get(e2.client, location(resp))
	if !strings.Contains(body, "Gmail was not connected") || !regexp.MustCompile(`<a id="retry"[^>]*href="/connect"`).MatchString(body) {
		t.Fatalf("denied page: %s", body)
	}
	if !strings.Contains(body, "one@example.com: Connected") {
		t.Error("the connected account vanished after a denial")
	}
}

func TestPreConsentRouteNeedsSignIn(t *testing.T) { // G30
	e := newEnv(t)
	resp, _ := e.get(e.newBrowser(), "/connect")
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("signed-out /connect = %d %q", resp.StatusCode, location(resp))
	}
}

func TestReconnectingARemovedAccountSaysReconnected(t *testing.T) { // G12, D5
	e := connectedEnv(t)
	e.advance(16 * time.Minute)
	e.setCheck(refreshOne, auth.ErrAccessRemoved)
	e.get(e.client, "/")
	again := grant("g-sub-1", "one@example.com")
	again.RefreshToken = "refresh-plaintext-newer"
	resp := e.connect(e.client, "g2", again)
	_, body := e.get(e.client, location(resp))
	if !strings.Contains(body, "reconnected") {
		t.Fatalf("no reconnected message: %s", body)
	}
	if !strings.Contains(body, "one@example.com: Connected") || strings.Contains(body, "Access was removed") {
		t.Errorf("account not back to connected: %s", body)
	}
	if e.mailboxes() != 1 {
		t.Errorf("mailboxes = %d", e.mailboxes())
	}
	tok, _ := e.st.RefreshToken(context.Background(), 1, "g-sub-1")
	if tok != "refresh-plaintext-newer" {
		t.Errorf("token = %q", tok)
	}
}

func bytesOf(b byte) []byte {
	out := make([]byte, 32)
	for i := range out {
		out[i] = b
	}
	return out
}
