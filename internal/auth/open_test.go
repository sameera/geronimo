package auth_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/sameera/geronimo/internal/store"
)

// fakeMarker stands in for Google's token endpoint and Gmail's mark-read request.
type fakeMarker struct {
	mu       sync.Mutex
	tokenErr error
	markErr  error
	refresh  []string // refresh tokens traded
	marks    []string // "<access token> <message id>"
}

func (f *fakeMarker) AccessToken(_ context.Context, refreshToken string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refresh = append(f.refresh, refreshToken)
	if f.tokenErr != nil {
		return "", f.tokenErr
	}
	return "access-for-" + refreshToken, nil
}

func (f *fakeMarker) MarkRead(_ context.Context, accessToken, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.marks = append(f.marks, accessToken+" "+id)
	return f.markErr
}

func (f *fakeMarker) markCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.marks...)
}

// rowFor returns the rendered row that opens the given message.
func rowFor(t *testing.T, body, id string) string {
	t.Helper()
	i := strings.Index(body, "#all/"+id+`"`)
	if i < 0 {
		t.Fatalf("no row opens %s:\n%s", id, body)
	}
	start := strings.LastIndex(body[:i], `<div class="email-row`)
	end := strings.Index(body[i:], "</div>")
	return body[start : i+end]
}

func attr(s, name string) string {
	i := strings.Index(s, name+`="`)
	if i < 0 {
		return ""
	}
	v := s[i+len(name)+2:]
	return strings.ReplaceAll(v[:strings.Index(v, `"`)], "&amp;", "&")
}

func (e *env) unread(id string) bool {
	e.t.Helper()
	emails, _, err := e.st.SenderEmails(context.Background(), 1, "ann@x.com", nil, 100)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, em := range emails {
		if em.MessageID == id {
			return em.Unread
		}
	}
	e.t.Fatalf("no stored email %s", id)
	return false
}

func TestTappingAnEmailOpensItInGmailInANewTab(t *testing.T) { // #43 AC1
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", "Ann <ann@x.com>", "", "UNREAD"), m("2", "Ann <ann@x.com>", ""))
	_, body := e.get(e.client, "/senders/emails?sender=ann%40x.com")
	for _, id := range []string{"1", "2"} {
		row := rowFor(t, body, id)
		if !strings.Contains(row, `<a class="email-open"`) {
			t.Errorf("row %s is not a link:\n%s", id, row)
		}
		if got := attr(row, "href"); got != "https://mail.google.com/mail/?authuser=one%40example.com#all/"+id {
			t.Errorf("row %s href = %q", id, got)
		}
		if attr(row, "target") != "_blank" || !strings.Contains(attr(row, "rel"), "noopener") {
			t.Errorf("row %s does not open a new tab safely:\n%s", id, row)
		}
	}
}

func TestEmailOpensInTheGmailAccountThatHoldsIt(t *testing.T) { // #43 AC3
	e := connectedEnv(t)
	e.connect(e.client, "g2", grant("g-sub-2", "two@example.com"))
	e.storeMail("one@example.com", m("1", "Ann <ann@x.com>", ""))
	e.storeMail("two@example.com", m("2", "Ann <ann@x.com>", ""))
	_, body := e.get(e.client, "/senders/emails?sender=ann%40x.com")
	if got := attr(rowFor(t, body, "1"), "href"); !strings.Contains(got, "authuser=one%40example.com#") {
		t.Errorf("email 1 opens %q", got)
	}
	if got := attr(rowFor(t, body, "2"), "href"); !strings.Contains(got, "authuser=two%40example.com#") {
		t.Errorf("email 2 opens %q", got)
	}
}

func TestTappingAnUnreadEmailMarksItReadInGmail(t *testing.T) { // #43 AC2
	e := connectedEnv(t)
	e.connect(e.client, "g2", grant("g-sub-2", "two@example.com"))
	e.storeMail("two@example.com", m("7", "Ann <ann@x.com>", "", "INBOX", "UNREAD"))
	_, body := e.get(e.client, "/senders/emails?sender=ann%40x.com")
	row := rowFor(t, body, "7")
	post := attr(row, "hx-post")
	if !strings.HasPrefix(post, "/emails/read?") || attr(row, "hx-trigger") != "click" || attr(row, "hx-swap") != "outerHTML" {
		t.Fatalf("unread row does not mark read on tap:\n%s", row)
	}
	resp, frag := e.do(e.client, "POST", post, e.htmxSameOrigin())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if got := e.mk.markCalls(); len(got) != 1 || got[0] != "access-for-refresh-plaintext-g-sub-2 7" {
		t.Fatalf("mark-read calls = %v", got)
	}
	if strings.Contains(frag, "<html") || strings.Contains(frag, `class="email-row unread"`) || !strings.Contains(frag, ">Read<") {
		t.Errorf("fragment does not show the email read:\n%s", frag)
	}
	if strings.Contains(frag, "hx-post") {
		t.Error("a read row must not mark read again")
	}
	if !strings.Contains(attr(rowFor(t, frag, "7"), "href"), "authuser=two%40example.com#all/7") {
		t.Error("swapped row lost its Gmail link")
	}
	if e.unread("7") {
		t.Error("stored email still unread")
	}
}

func TestReadEmailsDoNotMarkAgain(t *testing.T) { // #43
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", "Ann <ann@x.com>", "", "INBOX"))
	_, body := e.get(e.client, "/senders/emails?sender=ann%40x.com")
	if strings.Contains(rowFor(t, body, "1"), "hx-post") {
		t.Error("read row posts a mark-read")
	}
}

func TestRefusedReadMarkStillOpensAndTellsTheUser(t *testing.T) { // #43 AC4
	for name, setup := range map[string]func(*fakeMarker){
		"gmail refuses": func(f *fakeMarker) { f.markErr = errors.New("gmail: status 500") },
		"token fails":   func(f *fakeMarker) { f.tokenErr = errors.New("refresh: request failed") },
	} {
		t.Run(name, func(t *testing.T) {
			e := connectedEnv(t)
			setup(e.mk)
			e.storeMail("one@example.com", m("1", "Ann <ann@x.com>", "", "UNREAD"))
			_, body := e.get(e.client, "/senders/emails?sender=ann%40x.com")
			if !strings.Contains(body, `id="notice"`) {
				t.Fatal("page has no notice region")
			}
			row := rowFor(t, body, "1")
			resp, frag := e.do(e.client, "POST", attr(row, "hx-post"), e.htmxSameOrigin())
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d: the swap must still happen", resp.StatusCode)
			}
			notice := between(frag, `id="notice"`, "</p>")
			if !strings.Contains(frag, `hx-swap-oob="true"`) || !strings.Contains(notice, "not saved") || !strings.Contains(notice, "Subject 1") {
				t.Errorf("user is not told the read mark was not saved:\n%s", frag)
			}
			if !strings.Contains(attr(rowFor(t, frag, "1"), "href"), "#all/1") {
				t.Error("row lost its Gmail link")
			}
			if !e.unread("1") {
				t.Error("stored email marked read although Gmail was not")
			}
		})
	}
}

func TestMarkReadIsScopedToTheSignedInAccount(t *testing.T) { // #43
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", "Ann <ann@x.com>", "", "UNREAD"))
	other := e.newBrowser()
	e.signIn(other, "c2", store.Claims{Subject: "bob-sub", Email: "bob@example.com", Name: "Bob"})
	resp, _ := e.do(other, "POST", "/emails/read?mailbox=1&message=1", e.htmxSameOrigin())
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("foreign mark-read status %d", resp.StatusCode)
	}
	resp, _ = e.do(e.client, "POST", "/emails/read?mailbox=1&message=nope", e.htmxSameOrigin())
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("missing email status %d", resp.StatusCode)
	}
	resp, _ = e.do(e.client, "POST", "/emails/read?mailbox=x&message=1", e.htmxSameOrigin())
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad mailbox status %d", resp.StatusCode)
	}
	if len(e.mk.markCalls()) != 0 || !e.unread("1") {
		t.Error("a refused request reached Gmail or changed mail")
	}
}

func TestMarkReadNeedsSameOriginPostAndSession(t *testing.T) { // #43, G20
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", "Ann <ann@x.com>", "", "UNREAD"))
	resp, _ := e.do(e.client, "POST", "/emails/read?mailbox=1&message=1", map[string]string{"Sec-Fetch-Site": "cross-site"})
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site status %d", resp.StatusCode)
	}
	resp, _ = e.do(e.client, "GET", "/emails/read?mailbox=1&message=1", nil)
	if resp.StatusCode == http.StatusOK {
		t.Errorf("GET status %d", resp.StatusCode)
	}
	resp, _ = e.do(e.newBrowser(), "POST", "/emails/read?mailbox=1&message=1", e.htmxSameOrigin())
	if resp.Header.Get("HX-Redirect") != "/signin" {
		t.Errorf("signed-out request not refused: %d", resp.StatusCode)
	}
	if len(e.mk.markCalls()) != 0 {
		t.Error("a refused request reached Gmail")
	}
}

func (e *env) htmxSameOrigin() map[string]string {
	h := e.sameOrigin()
	h["HX-Request"] = "true"
	return h
}
