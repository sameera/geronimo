package store

import (
	"context"
	"testing"
)

func TestBaselineIsWrittenOnlyWhenTheFirstSyncFinishes(t *testing.T) { // G17
	st, _, mb := openSyncStore(t)
	ctx := context.Background()
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, mb, 10); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Baseline(ctx, mb); err != nil || ok {
		t.Fatalf("baseline before finish: ok=%v err=%v", ok, err)
	}
	if err := st.FinishSync(ctx, mb, 4); err != nil {
		t.Fatal(err)
	}
	b, ok, err := st.Baseline(ctx, mb)
	if err != nil || !ok || b.Unread != 4 {
		t.Fatalf("baseline after finish = %+v ok=%v err=%v", b, ok, err)
	}
}

func TestBaselineNeverChangesAfterItIsSet(t *testing.T) { // G18
	st, _, mb := openSyncStore(t)
	ctx := context.Background()
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, mb, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishSync(ctx, mb, 4); err != nil {
		t.Fatal(err)
	}
	// A later reconnect restarts nothing, and a stray finish changes nothing.
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishSync(ctx, mb, 9); err != nil {
		t.Fatal(err)
	}
	b, ok, err := st.Baseline(ctx, mb)
	if err != nil || !ok || b.Unread != 4 {
		t.Fatalf("baseline = %+v ok=%v err=%v", b, ok, err)
	}
	var n int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM baselines WHERE mailbox_id = ?`, mb).Scan(&n); err != nil || n != 1 {
		t.Fatalf("baseline rows = %d, %v", n, err)
	}
}

func TestFailedFirstSyncGetsItsBaselineOnlyOnFinish(t *testing.T) { // G17
	st, _, mb := openSyncStore(t)
	ctx := context.Background()
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, mb, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.StopSync(ctx, mb, StopOther); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Baseline(ctx, mb); ok {
		t.Fatal("stopped sync wrote a baseline")
	}
	if err := st.EnsureSync(ctx, mb); err != nil {
		t.Fatal(err)
	}
	if err := st.StartRun(ctx, mb, 10); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishSync(ctx, mb, 7); err != nil {
		t.Fatal(err)
	}
	if b, ok, _ := st.Baseline(ctx, mb); !ok || b.Unread != 7 {
		t.Fatalf("baseline after retry = %+v ok=%v", b, ok)
	}
}
