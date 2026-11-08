package auth_test

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestSignedInUserWithNoGmailSeesPreConsentScreen(t *testing.T) { // G25
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	resp, body := e.get(e.client, "/")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("home = %d", resp.StatusCode)
	}
	if !strings.Contains(body, `id="pre-consent"`) {
		t.Fatalf("home lacks the pre-consent screen: %s", body)
	}
	if !strings.Contains(body, "Connect Gmail") {
		t.Fatalf("home lacks a connect action: %s", body)
	}
}

func TestPreConsentConnectActionIsFullPagePost(t *testing.T) { // G25, D10
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/")
	form := regexp.MustCompile(`(?s)<form[^>]*id="connect-gmail"[^>]*>`).FindString(body)
	if form == "" {
		t.Fatalf("no connect form: %s", body)
	}
	if !strings.Contains(form, `method="post"`) || !strings.Contains(form, `action="/connect/google/start"`) {
		t.Errorf("connect form = %s", form)
	}
	if strings.Contains(form, "hx-") {
		t.Errorf("connect form uses HTMX, Google cannot load in a fragment: %s", form)
	}
}

func TestPreConsentCopyStatesWhatGeronimoDoesAndNeverDoes(t *testing.T) { // G2
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/")
	for _, want := range []string{
		"read, archive, and move email to trash",
		"never permanently deletes",
		"never reads message bodies",
		"short message previews",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pre-consent copy lacks %q", want)
		}
	}
}

func TestPreConsentPageFitsPhoneWidth(t *testing.T) { // G27
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/")
	if !strings.Contains(body, `name="viewport" content="width=device-width, initial-scale=1"`) {
		t.Error("page lacks a device-width viewport")
	}
	resp, css := e.get(e.client, "/static/app.css")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("css = %d", resp.StatusCode)
	}
	// No fixed pixel widths that could force horizontal scroll on a phone.
	if m := regexp.MustCompile(`(?m)(^|[;{\s])(min-)?width:\s*\d+(px|rem)`).FindString(css); m != "" {
		t.Errorf("css sets a fixed width: %q", m)
	}
	if !strings.Contains(css, "overflow-wrap") {
		t.Error("css lets long words overflow the screen")
	}
}

func TestSignedOutUserNeverSeesPreConsentScreen(t *testing.T) {
	e := newEnv(t)
	resp, body := e.get(e.newBrowser(), "/")
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("signed-out home = %d %q", resp.StatusCode, location(resp))
	}
	if strings.Contains(body, "pre-consent") {
		t.Error("signed-out response carries the pre-consent screen")
	}
}

func TestSignInPageStillSaysSignInGivesNoMailAccess(t *testing.T) { // G4
	e := newEnv(t)
	_, body := e.get(e.newBrowser(), "/signin")
	if !strings.Contains(body, "does not give access to your mail") {
		t.Error("sign-in page lost its identity-only note")
	}
}
