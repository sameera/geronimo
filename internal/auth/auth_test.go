package auth_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sameera/geronimo/internal/auth"
	"github.com/sameera/geronimo/internal/store"
)

const (
	sessionCookie = "__Host-geronimo_session"
	attemptCookie = "__Host-geronimo_attempt"
	idle          = 7 * 24 * time.Hour
	absolute      = 30 * 24 * time.Hour
)

// fakeProvider stands in for Google. Codes map to claims or errors.
type fakeProvider struct {
	mu      sync.Mutex
	claims  map[string]store.Claims
	fail    map[string]error
	lastURL string
}

func (p *fakeProvider) AuthURL(state, nonce, verifier string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastURL = "https://accounts.example/auth?state=" + url.QueryEscape(state)
	return p.lastURL, nil
}

func (p *fakeProvider) Exchange(_ context.Context, code, verifier, nonce string) (store.Claims, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.fail[code]; err != nil {
		return store.Claims{}, err
	}
	c, ok := p.claims[code]
	if !ok {
		return store.Claims{}, errors.New("unknown code")
	}
	if verifier == "" || nonce == "" {
		return store.Claims{}, errors.New("missing verifier or nonce")
	}
	return c, nil
}

type env struct {
	t      *testing.T
	st     *store.Store
	prov   *fakeProvider
	conn   *fakeConnector
	gm     *fakeGmail
	chk    *fakeChecker
	srv    *httptest.Server
	client *http.Client
	logs   *bytes.Buffer
	now    time.Time
	dbPath string
	mu     sync.Mutex
}

func (e *env) clock() time.Time {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.now
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.now = e.now.Add(d)
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{
		t:      t,
		now:    time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC),
		logs:   &bytes.Buffer{},
		dbPath: filepath.Join(t.TempDir(), "geronimo.db"),
		prov:   &fakeProvider{claims: map[string]store.Claims{}, fail: map[string]error{}},
		conn:   newFakeConnector(),
		gm:     &fakeGmail{},
		chk:    &fakeChecker{results: map[string]error{}},
	}
	e.open()
	t.Cleanup(func() { e.srv.Close(); e.st.Close() })
	return e
}

// open (re)creates the store, service and server over the same database file.
func (e *env) open() {
	st, err := store.Open(e.dbPath, e.clock)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := st.SetTokenKey(testTokenKey); err != nil {
		e.t.Fatal(err)
	}
	e.st = st
	svc := auth.New(auth.Options{ // BaseURL is set once the server has an address
		Store:            st,
		Provider:         e.prov,
		Connector:        e.conn,
		Profiler:         e.gm,
		Checker:          e.chk,
		Logger:           slog.New(slog.NewTextHandler(e.logs, nil)),
		IdleLifetime:     idle,
		AbsoluteLifetime: absolute,
	})
	app := http.NewServeMux()
	app.HandleFunc("GET /secret", func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "secret content") })
	e.srv = httptest.NewTLSServer(svc.Handler(app))
	svc.BaseURL = e.srv.URL
	e.client = e.newBrowser()
}

func (e *env) newBrowser() *http.Client {
	jar, _ := cookiejar.New(nil)
	base := *e.srv.Client() // a copy: each browser has its own cookie jar
	c := &base
	c.Jar = jar
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return c
}

func (e *env) do(c *http.Client, method, path string, hdr map[string]string) (*http.Response, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

func (e *env) get(c *http.Client, path string) (*http.Response, string) {
	return e.do(c, "GET", path, nil)
}

// start runs the sign-in start and returns the state sent to Google.
func (e *env) start(c *http.Client) string {
	e.t.Helper()
	resp, _ := e.get(c, "/auth/google/start")
	if resp.StatusCode != http.StatusSeeOther {
		e.t.Fatalf("start status = %d", resp.StatusCode)
	}
	u, err := url.Parse(resp.Header.Get("Location"))
	if err != nil || u.Query().Get("state") == "" {
		e.t.Fatalf("bad start redirect %q", resp.Header.Get("Location"))
	}
	return u.Query().Get("state")
}

// signIn runs a whole successful sign-in for the claims and returns the callback response.
func (e *env) signIn(c *http.Client, code string, claims store.Claims) *http.Response {
	e.t.Helper()
	e.prov.mu.Lock()
	e.prov.claims[code] = claims
	e.prov.mu.Unlock()
	state := e.start(c)
	resp, _ := e.get(c, "/auth/google/callback?code="+code+"&state="+url.QueryEscape(state))
	return resp
}

func (e *env) cookie(c *http.Client, name string) string {
	u, _ := url.Parse(e.srv.URL)
	for _, ck := range c.Jar.Cookies(u) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

func (e *env) accounts() int {
	n, err := e.st.CountAccounts(context.Background())
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

var alice = store.Claims{Subject: "sub-alice", Email: "alice@example.com", Name: "Alice"}

func location(r *http.Response) string { return r.Header.Get("Location") }

func TestFirstSignInCreatesAccountAndLandsOnHome(t *testing.T) {
	e := newEnv(t)
	resp := e.signIn(e.client, "c1", alice)
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/" {
		t.Fatalf("callback = %d %q", resp.StatusCode, location(resp))
	}
	if e.accounts() != 1 {
		t.Fatalf("accounts = %d", e.accounts())
	}
	home, body := e.get(e.client, "/")
	if home.StatusCode != 200 || !strings.Contains(body, "alice@example.com") {
		t.Fatalf("home = %d %q", home.StatusCode, body)
	}
}

func TestSessionCookieFlags(t *testing.T) { // G7
	e := newEnv(t)
	resp := e.signIn(e.client, "c1", alice)
	var got *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			got = c
		}
	}
	if got == nil {
		t.Fatal("no session cookie")
	}
	if !got.HttpOnly || !got.Secure || got.SameSite != http.SameSiteLaxMode || got.Path != "/" || got.Domain != "" {
		t.Fatalf("flags wrong: %+v", got)
	}
	if got.MaxAge != int(absolute.Seconds()) {
		t.Fatalf("Max-Age = %d, want persistent %d", got.MaxAge, int(absolute.Seconds()))
	}
	if len(got.Value) < 40 {
		t.Fatalf("token too short: %d", len(got.Value))
	}
}

func TestAttemptCookieFlags(t *testing.T) {
	e := newEnv(t)
	resp, _ := e.get(e.client, "/auth/google/start")
	for _, c := range resp.Cookies() {
		if c.Name == attemptCookie {
			if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.MaxAge != 600 {
				t.Fatalf("attempt cookie flags wrong: %+v", c)
			}
			return
		}
	}
	t.Fatal("no attempt cookie")
}

func TestReturningUserKeepsAccount(t *testing.T) { // G4, G5
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	changed := alice
	changed.Email = "alice@new.example"
	e.signIn(e.newBrowser(), "c2", changed)
	if e.accounts() != 1 {
		t.Fatalf("accounts = %d, want 1", e.accounts())
	}
	_, body := e.get(e.client, "/")
	if !strings.Contains(body, "alice@new.example") {
		t.Fatalf("email not refreshed: %q", body)
	}
}

func TestConcurrentSignInsMakeOneAccount(t *testing.T) { // G4
	e := newEnv(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		code := "code-" + string(rune('a'+i))
		go func() {
			defer wg.Done()
			e.signInQuiet(code)
		}()
	}
	wg.Wait()
	if e.accounts() != 1 {
		t.Fatalf("accounts = %d, want 1", e.accounts())
	}
}

// signInQuiet is signIn without t.Fatal, safe in goroutines.
func (e *env) signInQuiet(code string) {
	e.prov.mu.Lock()
	e.prov.claims[code] = alice
	e.prov.mu.Unlock()
	c := e.newBrowser()
	resp, err := c.Get(e.srv.URL + "/auth/google/start")
	if err != nil {
		return
	}
	resp.Body.Close()
	u, _ := url.Parse(resp.Header.Get("Location"))
	resp, err = c.Get(e.srv.URL + "/auth/google/callback?code=" + code + "&state=" + url.QueryEscape(u.Query().Get("state")))
	if err == nil {
		resp.Body.Close()
	}
}

func TestSessionSurvivesReloadAndRestart(t *testing.T) { // G6, G25
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.advance(6 * 24 * time.Hour)
	if resp, _ := e.get(e.client, "/"); resp.StatusCode != 200 {
		t.Fatalf("after 6 days status = %d", resp.StatusCode)
	}
	token := e.cookie(e.client, sessionCookie)

	// Restart: new store, service and server over the same file.
	e.srv.Close()
	e.st.Close()
	e.open()
	u, _ := url.Parse(e.srv.URL)
	e.client.Jar.SetCookies(u, []*http.Cookie{{Name: sessionCookie, Value: token, Path: "/", Secure: true}})
	if resp, body := e.get(e.client, "/"); resp.StatusCode != 200 || !strings.Contains(body, "alice@example.com") {
		t.Fatalf("after restart = %d", resp.StatusCode)
	}
}

func TestIdleExpiry(t *testing.T) { // G16
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.advance(idle + time.Minute)
	resp, _ := e.get(e.client, "/")
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("idle expired = %d %q", resp.StatusCode, location(resp))
	}
}

func TestAbsoluteExpiryDespiteUse(t *testing.T) { // G16
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	for day := 0; day < 29; day++ {
		e.advance(24 * time.Hour)
		if resp, _ := e.get(e.client, "/"); resp.StatusCode != 200 {
			t.Fatalf("day %d status = %d", day+1, resp.StatusCode)
		}
	}
	e.advance(25 * time.Hour) // 30 days and 1 hour since sign-in
	if resp, _ := e.get(e.client, "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("after absolute expiry status = %d", resp.StatusCode)
	}
}

func TestOnlyTokenHashIsStored(t *testing.T) { // G8
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	token := e.cookie(e.client, sessionCookie)
	hashes, err := e.st.SessionHashes(context.Background())
	if err != nil || len(hashes) != 1 {
		t.Fatalf("hashes = %v %v", hashes, err)
	}
	if hashes[0] == token || hashes[0] != store.HashToken(token) {
		t.Fatal("database holds something other than the token hash")
	}
}

func TestFixatedTokenNeverBecomesSession(t *testing.T) { // G9
	e := newEnv(t)
	u, _ := url.Parse(e.srv.URL)
	e.client.Jar.SetCookies(u, []*http.Cookie{{Name: sessionCookie, Value: "attacker-chosen", Path: "/", Secure: true}})
	e.signIn(e.client, "c1", alice)
	if got := e.cookie(e.client, sessionCookie); got == "attacker-chosen" || got == "" {
		t.Fatalf("session token after sign-in = %q", got)
	}
	attacker := e.newBrowser()
	attacker.Jar.SetCookies(u, []*http.Cookie{{Name: sessionCookie, Value: "attacker-chosen", Path: "/", Secure: true}})
	if resp, _ := e.get(attacker, "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("fixated token accepted: %d", resp.StatusCode)
	}
	// Two sign-ins never share a token.
	first := e.cookie(e.client, sessionCookie)
	e.signIn(e.client, "c2", alice)
	if e.cookie(e.client, sessionCookie) == first {
		t.Fatal("token reused across sign-ins")
	}
}

func TestSignedOutBrowserGetsNoSignedInPage(t *testing.T) { // G14
	e := newEnv(t)
	for _, path := range []string{"/", "/secret", "/anything/else", "/signout"} {
		resp, body := e.get(e.client, path)
		if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
			t.Errorf("%s = %d %q", path, resp.StatusCode, location(resp))
		}
		if strings.Contains(body, "secret content") {
			t.Errorf("%s leaked content", path)
		}
	}
}

func TestHTMXRefusalIsFullPageRedirect(t *testing.T) { // G15
	e := newEnv(t)
	resp, body := e.do(e.client, "GET", "/secret", map[string]string{"HX-Request": "true"})
	if resp.Header.Get("HX-Redirect") != "/signin" {
		t.Fatalf("HX-Redirect = %q", resp.Header.Get("HX-Redirect"))
	}
	if resp.StatusCode != 200 || strings.Contains(body, "Sign in with Google") || location(resp) != "" {
		t.Fatalf("htmx refusal must carry no body or plain redirect: %d %q", resp.StatusCode, body)
	}
}

func TestPublicRoutesNeedNoSession(t *testing.T) {
	e := newEnv(t)
	for _, path := range []string{"/signin", "/healthz", "/static/app.css", "/auth/google/start"} {
		resp, _ := e.get(e.newBrowser(), path)
		if resp.StatusCode == http.StatusSeeOther && location(resp) == "/signin" {
			t.Errorf("%s gated", path)
		}
	}
}

func TestSignedInResponsesAreNoStore(t *testing.T) { // G22
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	for _, path := range []string{"/", "/secret"} {
		resp, _ := e.get(e.client, path)
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Errorf("%s Cache-Control = %q", path, resp.Header.Get("Cache-Control"))
		}
	}
}

func TestSignedInPagesTurnOffHTMXSnapshots(t *testing.T) { // D9
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/")
	if !strings.Contains(body, `"historyCacheSize":0`) || !strings.Contains(body, `refreshOnHistoryMiss`) {
		t.Fatalf("home lacks htmx history config: %s", body)
	}
}

func TestSignInPageWhenSignedInGoesHome(t *testing.T) {
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	resp, _ := e.get(e.client, "/signin")
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/" {
		t.Fatalf("signin when signed in = %d %q", resp.StatusCode, location(resp))
	}
}

func assertFailed(t *testing.T, e *env, c *http.Client, resp *http.Response, wantMsg string) {
	t.Helper()
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin?msg="+wantMsg {
		t.Fatalf("callback = %d %q, want /signin?msg=%s", resp.StatusCode, location(resp), wantMsg)
	}
	if e.accounts() != 0 {
		t.Fatalf("account created on failure")
	}
	if e.cookie(c, sessionCookie) != "" {
		t.Fatalf("session cookie set on failure")
	}
	hashes, _ := e.st.SessionHashes(context.Background())
	if len(hashes) != 0 {
		t.Fatalf("session stored on failure")
	}
}

func TestCallbackIntegrity(t *testing.T) { // G11, G13
	e := newEnv(t)
	e.prov.claims["good"] = alice

	t.Run("missing state", func(t *testing.T) {
		e.start(e.client)
		resp, _ := e.get(e.client, "/auth/google/callback?code=good")
		assertFailed(t, e, e.client, resp, "failed")
	})
	t.Run("wrong state", func(t *testing.T) {
		e.start(e.client)
		resp, _ := e.get(e.client, "/auth/google/callback?code=good&state=nope")
		assertFailed(t, e, e.client, resp, "failed")
	})
	t.Run("other browser", func(t *testing.T) {
		state := e.start(e.client)
		resp, _ := e.get(e.newBrowser(), "/auth/google/callback?code=good&state="+state)
		assertFailed(t, e, e.client, resp, "failed")
	})
	t.Run("expired", func(t *testing.T) {
		c := e.newBrowser()
		state := e.start(c)
		e.advance(11 * time.Minute)
		resp, _ := e.get(c, "/auth/google/callback?code=good&state="+state)
		assertFailed(t, e, c, resp, "failed")
	})
	t.Run("replay", func(t *testing.T) {
		c := e.newBrowser()
		state := e.start(c)
		u, _ := url.Parse(e.srv.URL)
		binding := e.cookie(c, attemptCookie)
		first, _ := e.get(c, "/auth/google/callback?code=good&state="+state)
		if location(first) != "/" {
			t.Fatalf("first callback = %q", location(first))
		}
		before := e.accounts()
		replay := e.newBrowser()
		replay.Jar.SetCookies(u, []*http.Cookie{{Name: attemptCookie, Value: binding, Path: "/", Secure: true}})
		resp, _ := e.get(replay, "/auth/google/callback?code=good&state="+state)
		if location(resp) != "/signin?msg=failed" || e.cookie(replay, sessionCookie) != "" || e.accounts() != before {
			t.Fatalf("replay accepted: %d %q", resp.StatusCode, location(resp))
		}
	})
}

func TestFailuresReturnToSignInWithMessage(t *testing.T) { // G17
	e := newEnv(t)
	t.Run("cancel", func(t *testing.T) {
		c := e.newBrowser()
		state := e.start(c)
		resp, _ := e.get(c, "/auth/google/callback?error=access_denied&state="+state)
		assertFailed(t, e, c, resp, "cancelled")
		_, body := e.get(c, location(resp))
		if !strings.Contains(body, "You cancelled sign-in") {
			t.Fatalf("message missing: %s", body)
		}
	})
	t.Run("other provider error", func(t *testing.T) {
		c := e.newBrowser()
		state := e.start(c)
		resp, _ := e.get(c, "/auth/google/callback?error=server_error&state="+state)
		assertFailed(t, e, c, resp, "failed")
		_, body := e.get(c, location(resp))
		if !strings.Contains(body, "Sign-in didn&#39;t complete. Please try again.") {
			t.Fatalf("message missing: %s", body)
		}
	})
	t.Run("exchange fails", func(t *testing.T) {
		e.prov.fail["bad"] = errors.New("id token: signature invalid")
		c := e.newBrowser()
		state := e.start(c)
		resp, _ := e.get(c, "/auth/google/callback?code=bad&state="+state)
		assertFailed(t, e, c, resp, "failed")
	})
	t.Run("unknown message key is not echoed", func(t *testing.T) {
		_, body := e.get(e.newBrowser(), "/signin?msg=%3Cscript%3E")
		if strings.Contains(body, "<script>") || strings.Contains(body, "&lt;script") {
			t.Fatal("query text echoed")
		}
	})
}

func TestFailureLogsCarryNoCodesOrTokens(t *testing.T) { // G18
	e := newEnv(t)
	e.prov.fail["SECRET-CODE-123"] = errors.New("code exchange: provider returned status 400")
	c := e.newBrowser()
	state := e.start(c)
	e.get(c, "/auth/google/callback?code=SECRET-CODE-123&state="+state)
	if !strings.Contains(e.logs.String(), "sign-in failed") {
		t.Fatalf("failure not logged: %q", e.logs.String())
	}
	if strings.Contains(e.logs.String(), "SECRET-CODE-123") || strings.Contains(e.logs.String(), state) {
		t.Fatalf("log leaks code or state: %q", e.logs.String())
	}
}

func TestPurgeRemovesExpiredRows(t *testing.T) {
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.advance(31 * 24 * time.Hour)
	if err := e.st.PurgeExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	hashes, _ := e.st.SessionHashes(context.Background())
	if len(hashes) != 0 {
		t.Fatalf("expired session kept: %v", hashes)
	}
}

func jarSet(e *env, c *http.Client, name, value string) {
	u, _ := url.Parse(e.srv.URL)
	c.Jar.SetCookies(u, []*http.Cookie{{Name: name, Value: value, Path: "/", Secure: true}})
}
