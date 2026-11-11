package auth_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/sameera/geronimo/internal/store"
)

func (e *env) storeMail(mailboxEmail string, msgs ...store.Message) {
	e.t.Helper()
	ctx := context.Background()
	boxes, err := e.st.MailboxIDs(ctx, 1)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.st.StoreMessages(ctx, boxes[mailboxEmail], msgs, nil); err != nil {
		e.t.Fatal(err)
	}
}

func m(id, from, date string, labels ...string) store.Message {
	return store.Message{ID: id, ThreadID: "t" + id, Sender: from, Subject: "Subject " + id, Date: date, Labels: labels}
}

func TestSenderViewGroupsRows(t *testing.T) { // G1, G5, G6, G7, G14, D2
	e := connectedEnv(t)
	e.storeMail("one@example.com",
		m("1", "Ann <ann@x.com>", "Mon, 02 Jan 2006 15:04:05 +0000", "UNREAD"),
		m("2", "Ann <ANN@x.com>", "Tue, 03 Jan 2006 15:04:05 +0000"),
		m("3", "<b@x.com>", "Tue, 03 Jan 2006 15:04:05 +0000"),
	)
	resp, body := e.get(e.client, "/senders")
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("cache-control = %q", resp.Header.Get("Cache-Control"))
	}
	for _, want := range []string{`class="group-row"`, ">Ann<", ">ann@x.com<", `datetime="2006-01-03T15:04:05Z"`, "2006-01-03"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q:\n%s", want, body)
		}
	}
	if strings.Index(body, "ann@x.com") > strings.Index(body, "b@x.com") {
		t.Error("largest group not first")
	}
}

func TestSenderViewEscapesNames(t *testing.T) { // G23
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", `"<script>alert(1)</script>" <a@x.com>`, ""))
	_, body := e.get(e.client, "/senders")
	if strings.Contains(body, "<script>alert(1)") {
		t.Fatalf("name rendered as markup:\n%s", body)
	}
}

func TestSenderViewShowsMailSoFarWithStrip(t *testing.T) { // G8, D10
	e := connectedEnv(t)
	ctx := context.Background()
	boxes, _ := e.st.MailboxIDs(ctx, 1)
	mb := boxes["one@example.com"]
	if err := e.st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := e.st.StartRun(ctx, mb, 10); err != nil {
		t.Fatal(err)
	}
	e.storeMail("one@example.com", m("1", "a@x.com", ""))
	_, body := e.get(e.client, "/senders")
	if !strings.Contains(body, "a@x.com") || !strings.Contains(body, `id="sync-strip"`) || !strings.Contains(body, "Syncing one@example.com") {
		t.Fatalf("body:\n%s", body)
	}
}

func TestSenderViewEmptyStates(t *testing.T) { // G20
	e := newEnv(t)
	e.signIn(e.client, "c1", alice)
	_, body := e.get(e.client, "/senders")
	if !strings.Contains(body, `href="/connect"`) {
		t.Errorf("no connect link:\n%s", body)
	}
	e.connect(e.client, "g1", grant("g-sub-1", "one@example.com"))
	resp, body := e.get(e.client, "/senders")
	if resp.StatusCode != 200 || !strings.Contains(body, "still syncing") {
		t.Errorf("status %d, body:\n%s", resp.StatusCode, body)
	}
}

func TestSenderViewShowMoreRendersUpToCount(t *testing.T) { // D5
	e := connectedEnv(t)
	var msgs []store.Message
	for i := 0; i < 130; i++ {
		msgs = append(msgs, m(string(rune('a'+i%26))+strings.Repeat("x", i/26), "s"+pad(i)+"@x.com", ""))
	}
	e.storeMail("one@example.com", msgs...)
	_, body := e.get(e.client, "/senders")
	if n := strings.Count(body, `class="group-row"`); n != 100 {
		t.Fatalf("first chunk = %d rows", n)
	}
	if !strings.Contains(body, "hx-get") || !strings.Contains(body, "n=200") || !strings.Contains(body, "hx-replace-url") {
		t.Errorf("no show-more control:\n%s", body[len(body)-800:])
	}
	_, body = e.do(e.client, "GET", "/senders?n=200", map[string]string{"HX-Request": "true"})
	if n := strings.Count(body, `class="group-row"`); n != 130 {
		t.Fatalf("rows after show more = %d", n)
	}
	if strings.Contains(body, "<html") || strings.Contains(body, "n=300") {
		t.Errorf("fragment is a full page or still offers more")
	}
}

func pad(i int) string {
	s := "000" + string(rune('0'+i/100)) + string(rune('0'+i/10%10)) + string(rune('0'+i%10))
	return s
}

func TestSenderViewIsSignedInOnly(t *testing.T) { // G21, G22
	e := newEnv(t)
	resp, _ := e.get(e.client, "/senders")
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("status %d location %q", resp.StatusCode, location(resp))
	}
	resp, _ = e.do(e.client, "GET", "/senders", map[string]string{"HX-Request": "true"})
	if resp.Header.Get("HX-Redirect") != "/signin" {
		t.Errorf("no HX-Redirect")
	}
}

func TestSenderViewDoesNotChangeStoredMail(t *testing.T) { // G11
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", "a@x.com", ""))
	before, _ := e.st.MessageIDs(context.Background(), 1)
	gmailCalls := e.gm.calls
	e.get(e.client, "/senders")
	e.get(e.client, "/senders?n=200")
	after, _ := e.st.MessageIDs(context.Background(), 1)
	if len(before) != len(after) {
		t.Fatal("stored mail changed")
	}
	if e.gm.calls != gmailCalls {
		t.Errorf("gmail was called")
	}
}
