package auth_test

import (
	"strings"
	"testing"

	"github.com/sameera/geronimo/internal/store"
)

// refreshHref returns the address the email view reloads itself from on return.
func refreshHref(t *testing.T, page string) string {
	t.Helper()
	view := between(page, `id="email-view"`, ">")
	if strings.Contains(view, "hx-") {
		t.Fatalf("the view must be swapped only by the return script: %s", view)
	}
	h := attr(view, "data-refresh")
	if h == "" {
		t.Fatalf("email view has no refresh address: %s", view)
	}
	return h
}

func cursors(body string) []string {
	var out []string
	for _, part := range strings.Split(body, `data-cursor="`)[1:] {
		out = append(out, part[:strings.Index(part, `"`)])
	}
	return out
}

func TestReturningRefreshesTheListWithTheEmailRead(t *testing.T) { // #44 AC1, AC2
	e := connectedEnv(t)
	e.storeMail("one@example.com",
		m("1", "Ann <ann@x.com>", "Mon, 02 Jan 2006 10:00:00 +0000", "UNREAD"),
		m("2", "Ann <ann@x.com>", "Sun, 01 Jan 2006 10:00:00 +0000", "UNREAD"),
	)
	_, page := e.get(e.client, "/senders/emails?sender=ann%40x.com")
	if !strings.Contains(page, `<span class="unread">2 unread</span>`) {
		t.Fatalf("header lacks the sender's unread count:\n%s", page)
	}
	href := refreshHref(t, page)
	cur := cursors(page)
	if len(cur) != 2 {
		t.Fatalf("rows carry %d cursors", len(cur))
	}
	e.do(e.client, "POST", attr(rowFor(t, page, "1"), "hx-post"), e.htmxSameOrigin())

	_, frag := e.do(e.client, "GET", href+"&through="+cur[1], map[string]string{"HX-Request": "true"})
	if strings.Contains(frag, "<html") {
		t.Fatal("refresh returned a full page")
	}
	if strings.Contains(rowFor(t, frag, "1"), "unread") || !strings.Contains(rowFor(t, frag, "1"), ">Read<") {
		t.Errorf("refreshed list shows the opened email unread:\n%s", frag)
	}
	if !strings.Contains(frag, `<span class="unread">1 unread</span>`) {
		t.Errorf("sender's unread count is not one lower:\n%s", frag)
	}
	_, view := e.get(e.client, "/senders")
	if !strings.Contains(view, `<span class="unread">1 unread</span>`) {
		t.Errorf("sender view unread count is not one lower")
	}
}

func TestRefreshKeepsEveryShownRowAndTheSort(t *testing.T) { // #44 AC3
	e := connectedEnv(t)
	var msgs []store.Message
	for i := 0; i < 120; i++ {
		msgs = append(msgs, m("m"+pad(i), "big@x.com", ""))
	}
	e.storeMail("one@example.com", msgs...)
	_, page := e.get(e.client, "/senders/emails?sender=big%40x.com&sort=unread&n=200")
	href := refreshHref(t, page)
	if !strings.Contains(href, "sort=unread") || !strings.Contains(href, "n=200") || !strings.Contains(href, "sender=big%40x.com") {
		t.Errorf("refresh address drops the sender or the view: %s", href)
	}
	// Load a second chunk, as the user did before tapping an email.
	more := attr(between(page, `class="more"`, ">"), "hx-get")
	_, chunk := e.do(e.client, "GET", more, map[string]string{"HX-Request": "true"})
	shown := append(cursors(page), cursors(chunk)...)
	if len(shown) != 100 {
		t.Fatalf("shown = %d", len(shown))
	}

	_, frag := e.do(e.client, "GET", href+"&through="+shown[99], map[string]string{"HX-Request": "true"})
	if got := cursors(frag); len(got) != 100 || got[0] != shown[0] || got[99] != shown[99] {
		t.Fatalf("refresh shows %d rows, want the 100 shown", len(got))
	}
	next := attr(between(frag, `class="more"`, ">"), "hx-get")
	if !strings.Contains(next, "after=") || !strings.Contains(next, "sort=unread") || !strings.Contains(next, "n=200") {
		t.Errorf("refreshed Load more loses its place or the sort: %s", next)
	}
	_, rest := e.do(e.client, "GET", next, map[string]string{"HX-Request": "true"})
	if n := strings.Count(rest, `class="email-row`); n != 20 {
		t.Errorf("rows after the refreshed list = %d, want 20", n)
	}

	_, first := e.do(e.client, "GET", href, map[string]string{"HX-Request": "true"})
	if n := strings.Count(first, `class="email-row`); n != 50 || !strings.Contains(first, `id="email-list"`) {
		t.Errorf("refresh with no rows shown = %d rows", n)
	}
	resp, _ := e.do(e.client, "GET", href+"&through=bogus", map[string]string{"HX-Request": "true"})
	if resp.StatusCode != 400 {
		t.Errorf("bad cursor status %d", resp.StatusCode)
	}
}

func TestPageScriptRefreshesOnReturnAfterOpeningAnEmail(t *testing.T) { // #44 AC1, AC3, G18
	e := connectedEnv(t)
	_, page := e.get(e.client, "/senders")
	body := between(page, "<script>", "</script>")
	for _, want := range []string{"visibilitychange", "a.email-open", "data-refresh", "htmx.ajax", "through", "data-cursor"} {
		if !strings.Contains(body, want) {
			t.Errorf("page script lacks %q", want)
		}
	}
}
