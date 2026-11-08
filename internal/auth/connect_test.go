package auth_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sameera/geronimo/internal/auth"
	"github.com/sameera/geronimo/internal/gmail"
	"github.com/sameera/geronimo/internal/store"
)

const connectCookie = "__Host-geronimo_connect"

var testTokenKey = bytes.Repeat([]byte{7}, 32)

// fakeConnector stands in for Google's Gmail consent step.
type fakeConnector struct {
	mu        sync.Mutex
	grants    map[string]auth.Grant
	fail      map[string]error
	exchanges int
	lastURL   string
}

func newFakeConnector() *fakeConnector {
	return &fakeConnector{grants: map[string]auth.Grant{}, fail: map[string]error{}}
}

func (c *fakeConnector) AuthURL(state, nonce, verifier string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastURL = "https://accounts.example/connect?state=" + url.QueryEscape(state)
	return c.lastURL, nil
}

func (c *fakeConnector) Exchange(_ context.Context, code, verifier, nonce string) (auth.Grant, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exchanges++
	if err := c.fail[code]; err != nil {
		return auth.Grant{}, err
	}
	g, ok := c.grants[code]
	if !ok {
		return auth.Grant{}, errors.New("unknown code")
	}
	if verifier == "" || nonce == "" {
		return auth.Grant{}, errors.New("missing verifier or nonce")
	}
	return g, nil
}

type fakeGmail struct {
	mu     sync.Mutex
	err    error
	calls  int
	tokens []string
}

func (g *fakeGmail) Profile(_ context.Context, accessToken string) (gmail.Profile, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	g.tokens = append(g.tokens, accessToken)
	if g.err != nil {
		return gmail.Profile{}, g.err
	}
	return gmail.Profile{Email: "box@example.com"}, nil
}

func grant(sub, email string) auth.Grant {
	return auth.Grant{
		Subject: sub, Email: email,
		RefreshToken: "refresh-plaintext-" + sub, AccessToken: "access-plaintext-" + sub,
		Scopes: []string{"openid", "email", gmail.ModifyScope},
	}
}

func (e *env) connectStart(c *http.Client) *http.Response {
	e.t.Helper()
	resp, _ := e.do(c, "POST", "/connect/google/start", e.sameOrigin())
	return resp
}

func stateOf(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("connect start = %d %q", resp.StatusCode, location(resp))
	}
	u, err := url.Parse(location(resp))
	if err != nil || u.Query().Get("state") == "" {
		t.Fatalf("bad connect redirect %q", location(resp))
	}
	return u.Query().Get("state")
}

func (e *env) callback(c *http.Client, query string) *http.Response {
	e.t.Helper()
	resp, _ := e.get(c, "/connect/google/callback?"+query)
	return resp
}

// connect runs a whole connect for the grant and returns the callback response.
func (e *env) connect(c *http.Client, code string, g auth.Grant) *http.Response {
	e.t.Helper()
	e.conn.mu.Lock()
	e.conn.grants[code] = g
	e.conn.mu.Unlock()
	state := stateOf(e.t, e.connectStart(c))
	return e.callback(c, "code="+code+"&state="+url.QueryEscape(state))
}

func (e *env) mailboxes() int {
	e.t.Helper()
	n := 0
	for _, id := range []int64{1, 2, 3} {
		m, err := e.st.Mailboxes(context.Background(), id)
		if err != nil {
			e.t.Fatal(err)
		}
		n += len(m)
	}
	return n
}

var bob = bobClaims()

func TestConnectStartSendsSignedInUserToGoogle(t *testing.T) { // D2, G5
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	resp := e.connectStart(e.client)
	stateOf(t, resp)
	if !strings.HasPrefix(location(resp), "https://accounts.example/connect") {
		t.Fatalf("redirect = %q", location(resp))
	}
	if e.cookie(e.client, connectCookie) == "" {
		t.Error("no connect attempt cookie")
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
}

func TestConnectStartRequiresSignedInSameOriginPost(t *testing.T) { // G5, G30
	e := newEnv(t)
	signedOut := e.newBrowser()
	resp, _ := e.do(signedOut, "POST", "/connect/google/start", e.sameOrigin())
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("signed-out start = %d %q", resp.StatusCode, location(resp))
	}
	hx := e.sameOrigin()
	hx["HX-Request"] = "true"
	resp, _ = e.do(signedOut, "POST", "/connect/google/start", hx)
	if resp.Header.Get("HX-Redirect") != "/signin" {
		t.Errorf("signed-out HTMX start did not get a full-page redirect: %v", resp.Header)
	}

	e.signIn(e.client, "c1", alice)
	for name, hdr := range map[string]map[string]string{
		"no origin headers": nil,
		"cross-site":        {"Sec-Fetch-Site": "cross-site"},
		"same-site":         {"Sec-Fetch-Site": "same-site"},
		"foreign origin":    {"Origin": "https://evil.example"},
	} {
		resp, _ := e.do(e.client, "POST", "/connect/google/start", hdr)
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status = %d", name, resp.StatusCode)
		}
	}
	if resp, _ := e.get(e.client, "/connect/google/start"); resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET start = %d, want 404 or 405", resp.StatusCode)
	}
	e.conn.mu.Lock()
	defer e.conn.mu.Unlock()
	if e.conn.lastURL != "" {
		t.Error("a refused start still reached Google")
	}
}

func TestConnectCookieFlags(t *testing.T) { // G32
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	resp := e.connectStart(e.client)
	var got *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == connectCookie {
			got = c
		}
	}
	if got == nil || !got.Secure || !got.HttpOnly || got.Domain != "" || got.Path != "/" || got.MaxAge <= 0 {
		t.Fatalf("cookie = %+v", got)
	}
}

func TestSuccessfulConnectShowsGmailConnectedAndHidesPreConsent(t *testing.T) { // G9, G26
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	resp := e.connect(e.client, "g1", grant("g-sub", "box@example.com"))
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(location(resp), "/") || strings.HasPrefix(location(resp), "/signin") {
		t.Fatalf("callback = %d %q", resp.StatusCode, location(resp))
	}
	page, body := e.get(e.client, location(resp))
	if page.StatusCode != 200 {
		t.Fatalf("home = %d", page.StatusCode)
	}
	if !strings.Contains(body, "box@example.com") || !strings.Contains(body, "Connected") {
		t.Errorf("home does not show the connected account: %s", body)
	}
	if strings.Contains(body, `id="pre-consent"`) {
		t.Error("pre-consent screen still shown after connecting")
	}
	_, body = e.get(e.client, "/")
	if strings.Contains(body, `id="pre-consent"`) || !strings.Contains(body, "box@example.com") {
		t.Errorf("plain home after connect: %s", body)
	}
	if e.cookie(e.client, connectCookie) != "" {
		t.Error("connect cookie not cleared")
	}
}

func TestProfileRequestUsesTheAccessToken(t *testing.T) { // D7
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.connect(e.client, "g1", grant("g-sub", "box@example.com"))
	if e.gm.calls != 1 || e.gm.tokens[0] != "access-plaintext-g-sub" {
		t.Fatalf("profile calls = %d tokens = %v", e.gm.calls, e.gm.tokens)
	}
}

func TestPartialGrantStoresNothingAndNamesWhatIsMissing(t *testing.T) { // G9, G10
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	g := grant("g-sub", "box@example.com")
	g.Scopes = []string{"openid", "email"}
	resp := e.connect(e.client, "g1", g)
	if e.mailboxes() != 0 {
		t.Fatal("mailbox stored for a partial grant")
	}
	if e.gm.calls != 0 {
		t.Error("Gmail called for a partial grant")
	}
	_, body := e.get(e.client, location(resp))
	if !strings.Contains(body, "permission to read and manage your mail") {
		t.Errorf("message does not name the missing permission: %s", body)
	}
	if !strings.Contains(body, `id="retry"`) || !strings.Contains(body, `id="pre-consent"`) {
		t.Errorf("no retry or pre-consent screen: %s", body)
	}
}

func TestDeniedConsentShowsPlainMessageAndRetry(t *testing.T) { // G20
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	state := stateOf(t, e.connectStart(e.client))
	resp := e.callback(e.client, "error=access_denied&state="+url.QueryEscape(state))
	if e.conn.exchanges != 0 {
		t.Error("code exchanged after a denial")
	}
	page, body := e.get(e.client, location(resp))
	if page.StatusCode != 200 || !strings.Contains(body, "Gmail was not connected") || !strings.Contains(body, `id="retry"`) {
		t.Fatalf("denied page = %d %s", page.StatusCode, body)
	}
	if e.mailboxes() != 0 {
		t.Error("mailbox stored after a denial")
	}
}

func TestOtherFailuresShowOneGenericMessage(t *testing.T) { // G21, G23, D9
	cases := map[string]func(e *env, g auth.Grant) *http.Response{
		"provider error": func(e *env, g auth.Grant) *http.Response {
			state := stateOf(e.t, e.connectStart(e.client))
			return e.callback(e.client, "error=server_error&state="+url.QueryEscape(state))
		},
		"missing code": func(e *env, g auth.Grant) *http.Response {
			state := stateOf(e.t, e.connectStart(e.client))
			return e.callback(e.client, "state="+url.QueryEscape(state))
		},
		"exchange fails": func(e *env, g auth.Grant) *http.Response {
			e.conn.fail["g1"] = errors.New("boom")
			return e.connect(e.client, "g1", g)
		},
		"Gmail does not answer": func(e *env, g auth.Grant) *http.Response {
			e.gm.err = errors.New("gmail down")
			return e.connect(e.client, "g1", g)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.signIn(e.client, "c1", alice)
			resp := run(e, grant("g-sub", "box@example.com"))
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("callback = %d", resp.StatusCode)
			}
			page, body := e.get(e.client, location(resp))
			if page.StatusCode != 200 || !strings.Contains(body, "Gmail wasn&#39;t connected, please try again") || !strings.Contains(body, `id="retry"`) {
				t.Fatalf("failure page = %d %s", page.StatusCode, body)
			}
			if e.mailboxes() != 0 {
				t.Error("mailbox stored on failure")
			}
		})
	}
}

func TestCallbackIntegrityForConnect(t *testing.T) { // G6, G7, G8
	t.Run("no session: nothing exchanged, sent to sign in", func(t *testing.T) { // G7
		e := newEnv(t)
		e.signIn(e.client, "c1", alice)
		e.conn.grants["g1"] = grant("g-sub", "box@example.com")
		state := stateOf(t, e.connectStart(e.client))
		e.signOut(e.client, e.sameOrigin()) // the session ends while the user is at Google
		resp := e.callback(e.client, "code=g1&state="+url.QueryEscape(state))
		if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
			t.Fatalf("callback = %d %q", resp.StatusCode, location(resp))
		}
		if e.conn.exchanges != 0 || e.mailboxes() != 0 {
			t.Errorf("exchanges = %d mailboxes = %d", e.conn.exchanges, e.mailboxes())
		}
	})
	t.Run("different signed-in account", func(t *testing.T) { // G8
		e := newEnv(t)
		e.signIn(e.client, "c1", alice)
		e.conn.grants["g1"] = grant("g-sub", "box@example.com")
		state := stateOf(t, e.connectStart(e.client))
		e.signOut(e.client, e.sameOrigin())
		e.signIn(e.client, "c2", bob)
		resp := e.callback(e.client, "code=g1&state="+url.QueryEscape(state))
		if e.conn.exchanges != 0 || e.mailboxes() != 0 {
			t.Errorf("exchanges = %d mailboxes = %d", e.conn.exchanges, e.mailboxes())
		}
		_, body := e.get(e.client, location(resp))
		if !strings.Contains(body, "Gmail wasn&#39;t connected, please try again") {
			t.Errorf("no generic failure: %s", body)
		}
	})
	t.Run("wrong browser", func(t *testing.T) { // G6
		e := newEnv(t)
		e.signIn(e.client, "c1", alice)
		e.conn.grants["g1"] = grant("g-sub", "box@example.com")
		state := stateOf(t, e.connectStart(e.client))
		other := e.newBrowser()
		e.signIn(other, "c3", alice)
		e.callback(other, "code=g1&state="+url.QueryEscape(state))
		if e.conn.exchanges != 0 || e.mailboxes() != 0 {
			t.Errorf("exchanges = %d mailboxes = %d", e.conn.exchanges, e.mailboxes())
		}
	})
	t.Run("replay", func(t *testing.T) { // G6
		e := newEnv(t)
		e.signIn(e.client, "c1", alice)
		e.conn.grants["g1"] = grant("g-sub", "box@example.com")
		state := stateOf(t, e.connectStart(e.client))
		e.callback(e.client, "code=g1&state="+url.QueryEscape(state))
		jarSet(e, e.client, connectCookie, "stale")
		e.callback(e.client, "code=g1&state="+url.QueryEscape(state))
		if e.conn.exchanges != 1 {
			t.Errorf("exchanges = %d, want 1", e.conn.exchanges)
		}
	})
	t.Run("expired", func(t *testing.T) { // G6
		e := newEnv(t)
		e.signIn(e.client, "c1", alice)
		e.conn.grants["g1"] = grant("g-sub", "box@example.com")
		state := stateOf(t, e.connectStart(e.client))
		e.advance(10 * time.Minute)
		e.callback(e.client, "code=g1&state="+url.QueryEscape(state))
		if e.conn.exchanges != 0 || e.mailboxes() != 0 {
			t.Errorf("exchanges = %d mailboxes = %d", e.conn.exchanges, e.mailboxes())
		}
	})
	t.Run("missing or forged state", func(t *testing.T) { // G6
		e := newEnv(t)
		e.signIn(e.client, "c1", alice)
		e.conn.grants["g1"] = grant("g-sub", "box@example.com")
		stateOf(t, e.connectStart(e.client))
		e.callback(e.client, "code=g1")
		e.callback(e.client, "code=g1&state=forged")
		if e.conn.exchanges != 0 || e.mailboxes() != 0 {
			t.Errorf("exchanges = %d mailboxes = %d", e.conn.exchanges, e.mailboxes())
		}
	})
	t.Run("sign-in attempts and connect attempts are not interchangeable", func(t *testing.T) { // D2
		e := newEnv(t)
		e.signIn(e.client, "c1", alice)
		e.conn.grants["g1"] = grant("g-sub", "box@example.com")
		e.prov.claims["g1"] = alice
		signInState := e.start(e.client)
		jarSet(e, e.client, connectCookie, e.cookie(e.client, attemptCookie))
		e.callback(e.client, "code=g1&state="+url.QueryEscape(signInState))
		if e.conn.exchanges != 0 || e.mailboxes() != 0 {
			t.Errorf("a sign-in attempt finished a connect")
		}
		connectState := stateOf(t, e.connectStart(e.client))
		jarSet(e, e.client, attemptCookie, e.cookie(e.client, connectCookie))
		resp, _ := e.get(e.client, "/auth/google/callback?code=g1&state="+url.QueryEscape(connectState))
		if location(resp) != "/signin?msg=failed" {
			t.Errorf("a connect attempt finished a sign-in: %q", location(resp))
		}
	})
}

func TestConnectRoutesAreGatedAndNoStore(t *testing.T) { // G30, G31
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	resp := e.connect(e.client, "g1", grant("g-sub", "box@example.com"))
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("callback Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
	out := e.newBrowser()
	if resp, _ := e.get(out, "/"); location(resp) != "/signin" {
		t.Errorf("signed-out home = %q", location(resp))
	}
}

func TestMessageKeysAreFixedAndNeverEchoed(t *testing.T) { // G33
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/?msg=%3Cscript%3Ealert(1)%3C/script%3E")
	if strings.Contains(body, "alert(1)") || strings.Contains(body, "&lt;script&gt;alert") {
		t.Fatalf("query text echoed: %s", body)
	}
	_, body = e.get(e.client, "/?msg=connect_failed&text=hello-world")
	if strings.Contains(body, "hello-world") {
		t.Fatal("extra query text echoed")
	}
}

func TestSecretsNeverReachLogsPagesOrDatabase(t *testing.T) { // G14, G15, G17, G36
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.conn.fail["bad"] = errors.New("code exchange: provider returned status 400")
	state := stateOf(t, e.connectStart(e.client))
	_, page := e.get(e.client, location(e.callback(e.client, "code=bad&state="+url.QueryEscape(state))))
	resp := e.connect(e.client, "g1", grant("g-sub", "box@example.com"))
	_, page2 := e.get(e.client, location(resp))
	all := page + page2 + e.logs.String()
	for _, secret := range []string{"g1", "access-plaintext-g-sub", "refresh-plaintext-g-sub"} {
		// "g1" is the OAuth code. It must not be logged or rendered.
		if strings.Contains(all, "code=g1") || (secret != "g1" && strings.Contains(all, secret)) {
			t.Errorf("%q appears in a log or page", secret)
		}
	}
	for _, suffix := range []string{"", "-wal"} {
		b, err := os.ReadFile(e.dbPath + suffix)
		if err != nil {
			continue
		}
		for _, secret := range []string{"refresh-plaintext-g-sub", "access-plaintext-g-sub"} {
			if bytes.Contains(b, []byte(secret)) {
				t.Errorf("%q found in database file %q", secret, suffix)
			}
		}
	}
	got, err := e.st.RefreshToken(context.Background(), 1, "g-sub")
	if err != nil || got != "refresh-plaintext-g-sub" {
		t.Errorf("stored token = %q, %v", got, err)
	}
}

func bobClaims() store.Claims {
	return store.Claims{Subject: "sub-bob", Email: "bob@example.com", Name: "Bob"}
}
