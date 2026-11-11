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

func TestEmailListShowsSenderEmails(t *testing.T) { // #41, D2, G12, G23
	e := connectedEnv(t)
	bad := m("3", "Ann <ann@x.com>", "Wed, 04 Jan 2006 15:04:05 +0000")
	bad.Subject = "<b>bold</b>"
	empty := m("4", "ann@x.com", "")
	empty.Subject = ""
	e.storeMail("one@example.com",
		m("1", "Ann <ann@x.com>", "Mon, 02 Jan 2006 15:04:05 +0000", "UNREAD"),
		m("2", "Ann <ann@x.com>", "Tue, 03 Jan 2006 15:04:05 +0000"),
		bad, empty,
		m("9", "other@x.com", ""),
	)
	resp, body := e.get(e.client, "/senders/emails?sender=ann%40x.com")
	if resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("status %d cache %q", resp.StatusCode, resp.Header.Get("Cache-Control"))
	}
	for _, want := range []string{"Ann", "ann@x.com", "Subject 1", "Subject 2", "&lt;b&gt;bold&lt;/b&gt;", "(no subject)", `datetime="2006-01-02T15:04:05Z"`, "Unread", "Read"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "Subject 9") || strings.Contains(body, "<b>bold") {
		t.Error("foreign email or markup rendered")
	}
	if strings.Count(body, `class="email-row`) != 4 {
		t.Errorf("rows = %d", strings.Count(body, `class="email-row`))
	}
	if strings.Contains(body, `class="email-row`) && strings.Contains(between(body, `class="email-row`, "</div>"), "<a ") {
		t.Error("email rows must not be links")
	}
}

func between(s, from, to string) string {
	i := strings.Index(s, from)
	j := strings.Index(s[i:], to)
	return s[i : i+j]
}

func TestEmailListCountMatchesGroupTotalAndLoadsMore(t *testing.T) { // #41, G3, G19, D8
	e := connectedEnv(t)
	var msgs []store.Message
	for i := 0; i < 120; i++ {
		msgs = append(msgs, m("m"+pad(i), "big@x.com", ""))
	}
	e.storeMail("one@example.com", msgs...)
	_, view := e.get(e.client, "/senders")
	if !strings.Contains(view, `<span class="total">120</span>`) {
		t.Fatalf("group total missing:\n%s", view)
	}
	_, body := e.get(e.client, "/senders/emails?sender=big%40x.com")
	if !strings.Contains(body, "120 emails") {
		t.Errorf("header lacks total")
	}
	if n := strings.Count(body, `class="email-row`); n != 50 {
		t.Fatalf("first page = %d rows", n)
	}
	total := 50
	for i := 0; i < 5 && strings.Contains(body, `class="more"`); i++ {
		i1 := strings.Index(body, `hx-get="`)
		href := body[i1+len(`hx-get="`):]
		href = strings.ReplaceAll(href[:strings.Index(href, `"`)], "&amp;", "&")
		_, body = e.do(e.client, "GET", href, map[string]string{"HX-Request": "true"})
		if strings.Contains(body, "<html") {
			t.Fatal("fragment is a full page")
		}
		total += strings.Count(body, `class="email-row`)
	}
	if total != 120 {
		t.Fatalf("rows reached = %d", total)
	}
}

func TestEmailListUnknownSenderIsEmptyNotError(t *testing.T) { // G10
	e := connectedEnv(t)
	resp, body := e.get(e.client, "/senders/emails?sender=nobody%40x.com")
	if resp.StatusCode != 200 || strings.Contains(body, `class="email-row`) {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestEmailListIsSignedInOnly(t *testing.T) { // G21
	e := newEnv(t)
	resp, _ := e.get(e.client, "/senders/emails?sender=a%40x.com")
	if resp.StatusCode != http.StatusSeeOther || location(resp) != "/signin" {
		t.Fatalf("status %d location %q", resp.StatusCode, location(resp))
	}
}

func hrefs(body, class string) []string {
	var out []string
	for _, part := range strings.Split(body, `class="`+class+`" href="`)[1:] {
		out = append(out, strings.ReplaceAll(part[:strings.Index(part, `"`)], "&amp;", "&"))
	}
	return out
}

func TestSenderViewSorts(t *testing.T) { // #42, D6, G15, G16
	e := connectedEnv(t)
	e.storeMail("one@example.com",
		m("1", "big@x.com", "Mon, 02 Jan 2006 10:00:00 +0000"), m("2", "big@x.com", "Mon, 02 Jan 2006 11:00:00 +0000"),
		m("3", "unr@x.com", "Tue, 03 Jan 2006 10:00:00 +0000", "UNREAD"),
		m("4", "new@x.com", "Fri, 06 Jan 2006 10:00:00 +0000"),
	)
	order := func(path string) []string {
		_, body := e.get(e.client, path)
		var out []string
		for _, h := range hrefs(body, "group-row") {
			out = append(out, strings.TrimPrefix(strings.SplitN(h, "&", 2)[0], "/senders/emails?sender="))
		}
		return out
	}
	join := func(s []string) string { return strings.Join(s, ",") }
	if got := join(order("/senders")); got != "big%40x.com,new%40x.com,unr%40x.com" {
		t.Errorf("default = %s", got)
	}
	if got := join(order("/senders?sort=unread")); got != "unr%40x.com,big%40x.com,new%40x.com" {
		t.Errorf("unread = %s", got)
	}
	if got := join(order("/senders?sort=newest")); got != "new%40x.com,unr%40x.com,big%40x.com" {
		t.Errorf("newest = %s", got)
	}
	if got := join(order("/senders?sort=bogus")); got != "big%40x.com,new%40x.com,unr%40x.com" {
		t.Errorf("bogus sort = %s", got)
	}
}

func TestSortControlReplacesAddress(t *testing.T) { // #42, G16, D6
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", "a@x.com", ""))
	_, body := e.get(e.client, "/senders?sort=unread")
	for _, want := range []string{`href="/senders?sort=total"`, `href="/senders?sort=unread"`, `href="/senders?sort=newest"`} {
		if !strings.Contains(body, want) {
			t.Errorf("no sort control %s", want)
		}
	}
	if strings.Contains(body, "hx-push-url") || !strings.Contains(body, `hx-replace-url="true"`) {
		t.Error("sort change must replace the address, not push")
	}
	if !strings.Contains(body, `aria-current="true"`) {
		t.Error("chosen sort not marked")
	}
}

func TestStateTravelsToListAndBack(t *testing.T) { // #42, G17
	e := connectedEnv(t)
	e.storeMail("one@example.com", m("1", "a@x.com", ""), m("2", "b@x.com", ""))
	_, body := e.get(e.client, "/senders?sort=unread&n=200")
	rows := hrefs(body, "group-row")
	if len(rows) != 2 || !strings.Contains(rows[0], "sort=unread") || !strings.Contains(rows[0], "n=200") {
		t.Fatalf("row hrefs = %v", rows)
	}
	_, list := e.get(e.client, rows[0])
	if !strings.Contains(list, `id="back"`) || !strings.Contains(list, `href="/senders?n=200&amp;sort=unread"`) {
		t.Errorf("list lacks back link to the exact view:\n%s", list)
	}
}

func TestPageScriptSavesOnlyScrollPositions(t *testing.T) { // #42, G17, G18
	e := connectedEnv(t)
	_, page := e.get(e.client, "/senders")
	body := between(page, "<script>", "</script>")
	for _, want := range []string{"sessionStorage.setItem", "sessionStorage.getItem", "scrollY", "scrollTo", "history.back"} {
		if !strings.Contains(body, want) {
			t.Errorf("page script lacks %q", want)
		}
	}
	// The sender view's saved offset is keyed on sort only, so /senders, /senders?sort=X and
	// /senders?n=<shown> all find the same offset on back (G17).
	if !strings.Contains(body, `"scroll:/senders?sort="`) || !strings.Contains(body, `get("sort") || "total"`) {
		t.Error("sender view scroll key must be built from sort, not the raw address")
	}
	for _, bad := range []string{"localStorage", "innerHTML", "textContent.", "document.cookie"} {
		if strings.Contains(strings.ReplaceAll(body, "el.textContent = d.toLocaleDateString()", ""), bad) {
			t.Errorf("page script uses %q", bad)
		}
	}
}
