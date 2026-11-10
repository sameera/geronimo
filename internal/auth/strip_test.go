package auth

import (
	"strings"
	"testing"

	"github.com/sameera/geronimo/internal/store"
)

func TestStripShowsSyncedOutOfTotal(t *testing.T) { // G14
	v := buildStrip([]store.SyncRecord{{
		MailboxID: 1, Email: "me@example.com", State: store.SyncRunning, InboxTotal: 31000, SyncedCount: 12400,
	}})
	if len(v.Lines) != 1 || !strings.Contains(v.Lines[0], "12,400 of 31,000 emails") {
		t.Fatalf("lines = %v", v.Lines)
	}
	if !v.Poll {
		t.Fatal("strip does not poll while a sync runs")
	}
}

func TestSyncedCountIsNeverShownAboveTotal(t *testing.T) { // G14, D7
	v := buildStrip([]store.SyncRecord{{
		MailboxID: 1, Email: "me@example.com", State: store.SyncRunning, InboxTotal: 30, SyncedCount: 40,
	}})
	if len(v.Lines) != 1 || !strings.Contains(v.Lines[0], "40 of 40 emails") {
		t.Fatalf("lines = %v", v.Lines)
	}
}

func TestStartingSyncShowsOnFirstLoad(t *testing.T) { // G13
	v := buildStrip([]store.SyncRecord{{MailboxID: 1, Email: "me@example.com", State: store.SyncStarting}})
	if len(v.Lines) != 1 || !strings.Contains(v.Lines[0], "me@example.com") || !v.Poll {
		t.Fatalf("strip = %+v", v)
	}
}

func TestDoneShowsOnceThenGoes(t *testing.T) { // G15
	v := buildStrip([]store.SyncRecord{{MailboxID: 7, Email: "me@example.com", State: store.SyncDone}})
	if len(v.Lines) != 1 || !strings.Contains(v.Lines[0], "Sync done") || !v.Poll {
		t.Fatalf("first view = %+v", v)
	}
	if len(v.Ack) != 1 || v.Ack[0] != 7 {
		t.Fatalf("ack = %v", v.Ack)
	}
	later := buildStrip([]store.SyncRecord{{MailboxID: 7, Email: "me@example.com", State: store.SyncDone, StripAcked: true}})
	if len(later.Lines) != 0 || later.Poll || len(later.Ack) != 0 {
		t.Fatalf("later view = %+v", later)
	}
}

func TestStoppedSyncsAreNotInTheStrip(t *testing.T) {
	v := buildStrip([]store.SyncRecord{{MailboxID: 1, Email: "me@example.com", State: store.SyncStopped, StopReason: store.StopOther}})
	if len(v.Lines) != 0 || v.Poll {
		t.Fatalf("strip = %+v", v)
	}
}

func TestIdleAccountHasNoStripAndNoPolling(t *testing.T) {
	v := buildStrip(nil)
	if len(v.Lines) != 0 || v.Poll || len(v.Ack) != 0 {
		t.Fatalf("strip = %+v", v)
	}
}

func TestFormatCountGroupsThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1,000", 12400: "12,400", 1234567: "1,234,567"} {
		if got := formatCount(n); got != want {
			t.Errorf("formatCount(%d) = %q, want %q", n, got, want)
		}
	}
}
