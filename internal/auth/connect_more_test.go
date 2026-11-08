package auth_test

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestConnectAnotherActionShownOnlyWithAConnectedAccount(t *testing.T) { // G26
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/")
	if strings.Contains(body, `id="connect-another"`) {
		t.Error("connect-another shown before any account is connected")
	}
	e.connect(e.client, "g1", grant("g-sub-1", "one@example.com"))
	_, body = e.get(e.client, "/")
	form := regexp.MustCompile(`(?s)<form[^>]*id="connect-another"[^>]*>`).FindString(body)
	if form == "" {
		t.Fatalf("no connect-another action: %s", body)
	}
	if !strings.Contains(form, `method="post"`) || !strings.Contains(form, `action="/connect/google/start"`) || strings.Contains(form, "hx-") {
		t.Errorf("connect-another form = %s", form)
	}
	if strings.Contains(body, `id="pre-consent"`) {
		t.Error("pre-consent shown to a user with a connected account")
	}
}

func TestConnectingASecondAccountShowsBoth(t *testing.T) { // G13
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.connect(e.client, "g1", grant("g-sub-1", "one@example.com"))
	resp := e.connect(e.client, "g2", grant("g-sub-2", "two@example.com"))
	if resp.StatusCode != http.StatusSeeOther || strings.HasPrefix(location(resp), "/signin") {
		t.Fatalf("second connect = %d %q", resp.StatusCode, location(resp))
	}
	_, body := e.get(e.client, "/")
	for _, want := range []string{"one@example.com: Connected", "two@example.com: Connected"} {
		if !strings.Contains(body, want) {
			t.Errorf("home lacks %q: %s", want, body)
		}
	}
	if e.gm.calls != 2 {
		t.Errorf("Gmail profile calls = %d, want one per connect", e.gm.calls)
	}
}

func TestConnectingAConnectedAccountAddsNothingAndSaysSo(t *testing.T) { // G11, G12
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	e.connect(e.client, "g1", grant("g-sub-1", "one@example.com"))
	again := grant("g-sub-1", "one@example.com")
	again.RefreshToken = "refresh-plaintext-newer"
	resp := e.connect(e.client, "g2", again)
	page, body := e.get(e.client, location(resp))
	if page.StatusCode != 200 || !strings.Contains(body, "already connected") {
		t.Fatalf("no already-connected message (%d): %s", page.StatusCode, body)
	}
	if strings.Contains(body, `id="retry"`) {
		t.Error("already-connected is not a failure and needs no retry")
	}
	if strings.Count(body, "one@example.com: Connected") != 1 {
		t.Errorf("account listed more than once: %s", body)
	}
	if e.mailboxes() != 1 {
		t.Fatalf("mailboxes = %d", e.mailboxes())
	}
	tok, err := e.st.RefreshToken(context.Background(), 1, "g-sub-1")
	if err != nil || tok != "refresh-plaintext-newer" {
		t.Errorf("stored token = %q, %v", tok, err)
	}
}

func TestAlreadyConnectedMessageIsAFixedKey(t *testing.T) { // G33
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/?msg=connect_already&x=injected")
	if !strings.Contains(body, "already connected") || strings.Contains(body, "injected") {
		t.Errorf("body = %s", body)
	}
}
