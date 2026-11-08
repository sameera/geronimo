package auth_test

import (
	"net/http"
	"strings"
	"testing"
)

func (e *env) signOut(c *http.Client, hdr map[string]string) *http.Response {
	e.t.Helper()
	resp, _ := e.do(c, "POST", "/signout", hdr)
	return resp
}

func (e *env) sameOrigin() map[string]string {
	return map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": e.srv.URL}
}

func TestSignOutEndsSessionAndShowsSignInPage(t *testing.T) {
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	if _, body := e.get(e.client, "/"); !strings.Contains(body, `action="/signout"`) {
		t.Fatalf("home has no sign-out form: %s", body)
	}
	resp := e.signOut(e.client, e.sameOrigin())
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin?msg=signed_out" {
		t.Fatalf("sign-out = %d %q", resp.StatusCode, location(resp))
	}
	page, body := e.get(e.client, location(resp))
	if page.StatusCode != 200 || !strings.Contains(body, "Sign in with Google") {
		t.Fatalf("sign-in page = %d", page.StatusCode)
	}
	if !strings.Contains(body, "may still be signed in to Google") {
		t.Fatalf("note about Google missing: %s", body)
	}
	if home, _ := e.get(e.client, "/"); home.StatusCode != http.StatusSeeOther || location(home) != "/signin" {
		t.Fatalf("home after sign-out = %d %q", home.StatusCode, location(home))
	}
	if e.cookie(e.client, sessionCookie) != "" {
		t.Fatal("session cookie not cleared")
	}
}

func TestSignOutDeletesServerSideSession(t *testing.T) { // G19
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	old := e.cookie(e.client, sessionCookie)
	e.signOut(e.client, e.sameOrigin())

	replay := e.newBrowser()
	setSession(e, replay, old)
	if resp, _ := e.get(replay, "/"); resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("replayed token accepted: %d", resp.StatusCode)
	}
}

func TestSignOutOnlyAcceptsSameOriginPost(t *testing.T) { // G20
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	stillSignedIn := func(label string) {
		t.Helper()
		if resp, _ := e.get(e.client, "/"); resp.StatusCode != 200 {
			t.Fatalf("%s signed the user out", label)
		}
	}

	cases := map[string]map[string]string{
		"cross-site fetch metadata": {"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"},
		"same-site subdomain":       {"Sec-Fetch-Site": "same-site"},
		"foreign origin":            {"Origin": "https://evil.example"},
		"no origin information":     {},
	}
	for name, hdr := range cases {
		if resp := e.signOut(e.client, hdr); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, resp.StatusCode)
		}
		stillSignedIn(name)
	}

	// A GET never signs out.
	e.get(e.client, "/signout")
	stillSignedIn("GET /signout")

	// Origin alone is enough when the browser sends no Fetch-Metadata.
	if resp := e.signOut(e.client, map[string]string{"Origin": e.srv.URL}); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("origin-only sign-out = %d", resp.StatusCode)
	}
}

func TestSignOutLeavesOtherBrowsersSignedIn(t *testing.T) { // G21
	e := newEnv(t)
	other := e.newBrowser()
	e.signIn(e.client, "c1", alice)
	e.signIn(other, "c2", alice)
	e.signOut(e.client, e.sameOrigin())
	if resp, _ := e.get(other, "/"); resp.StatusCode != 200 {
		t.Fatalf("other browser = %d", resp.StatusCode)
	}
	if resp, _ := e.get(e.client, "/"); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("signed-out browser = %d", resp.StatusCode)
	}
}

func TestBackButtonShowsNoSignedInContent(t *testing.T) { // G23
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	// A full page and an HTMX-navigated page, viewed while signed in.
	for _, path := range []string{"/", "/secret"} {
		resp, _ := e.get(e.client, path)
		if resp.Header.Get("Cache-Control") != "no-store" {
			t.Fatalf("%s is cacheable", path)
		}
	}
	e.signOut(e.client, e.sameOrigin())

	// Back: the browser asks the server again.
	for _, path := range []string{"/", "/secret"} {
		resp, body := e.get(e.client, path)
		if resp.StatusCode != http.StatusSeeOther || strings.Contains(body, "secret content") || strings.Contains(body, "alice@example.com") {
			t.Errorf("full page %s after sign-out: %d %q", path, resp.StatusCode, body)
		}
		resp, body = e.do(e.client, "GET", path, map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"})
		if resp.Header.Get("HX-Redirect") != "/signin" || strings.Contains(body, "secret content") || strings.Contains(body, "alice@example.com") {
			t.Errorf("htmx restore %s after sign-out: %d %q", path, resp.StatusCode, body)
		}
	}
}

func TestSignOutResponseAsksBrowserToClearCaches(t *testing.T) { // D9, defence in depth
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	resp := e.signOut(e.client, e.sameOrigin())
	if got := resp.Header.Get("Clear-Site-Data"); !strings.Contains(got, `"cache"`) || !strings.Contains(got, `"storage"`) {
		t.Fatalf("Clear-Site-Data = %q", got)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
}

func TestSignedOutSignOutGoesToSignIn(t *testing.T) {
	e := newEnv(t)
	resp := e.signOut(e.client, e.sameOrigin())
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("signed-out sign-out = %d %q", resp.StatusCode, location(resp))
	}
}

func setSession(e *env, c *http.Client, token string) {
	jarSet(e, c, sessionCookie, token)
}
