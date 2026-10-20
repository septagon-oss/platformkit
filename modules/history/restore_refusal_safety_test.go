package history_test

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTwoRestoresOfOneRevisionCommitOnlyOneVersion(t *testing.T) {
	w := newWorld(t)
	router := w.mount(grants{memoRead: true, memoWrite: true})
	id := create(t, router, acmeHost, "First draft")
	if code, body := edit(t, router, acmeHost, id, "Second draft"); code != http.StatusOK {
		t.Fatalf("edit = %d %s", code, body)
	}
	codes := make([]int, 2)
	bodies := make([]string, 2)
	ready := make(chan struct{})
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() {
			<-ready
			codes[i], bodies[i] = restore(t, router, acmeHost, id, int64(i+1), 2)
		})
	}
	close(ready)
	wg.Wait()
	accepted, refused := 0, 0
	for i, code := range codes {
		switch code {
		case http.StatusOK:
			accepted++
		case http.StatusPreconditionFailed:
			refused++
			if strings.Contains(bodies[i], `"id"`) || strings.Contains(bodies[i], `"revision"`) {
				t.Errorf("stale restore returned a record: %s", bodies[i])
			}
		default:
			t.Errorf("restore %d = %d %s", i, code, bodies[i])
		}
	}
	if accepted != 1 || refused != 1 {
		t.Errorf("two restores accepted %d and refused %d, want one each", accepted, refused)
	}
	if versions := w.versionsOf(id); len(versions) != 3 {
		t.Errorf("two restores left %d versions, want 3", len(versions))
	}
	if _, revision := w.row(id); revision != 3 {
		t.Errorf("row revision = %d, want 3", revision)
	}
	if n := w.outbox(rest.RestoredEvent); n != 1 {
		t.Errorf("two restores emitted %d restored events, want 1", n)
	}
}

func TestAFailedVersionWriteRollsBackTheRecordAndItsEvents(t *testing.T) {
	w := newWorld(t)
	router := w.mount(grants{memoRead: true, memoWrite: true})
	id := create(t, router, acmeHost, "First draft")
	err := db.Run(tenancy.WithTenant(t.Context(), acme), w.app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.store.RecordVersion(ctx, tx, httpx.VersionInput{
			Module: "memos", Entity: "memo", Record: id, Locale: "und", Version: 2,
			Fields: []byte(`{"title":"Reserved version"}`),
		})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	before := w.versionsOf(id)
	code, body := edit(t, router, acmeHost, id, "Uncommitted draft")
	if code < http.StatusInternalServerError {
		t.Fatalf("duplicate version store failure = %d %s, want failure", code, body)
	}
	if strings.Contains(body, `"id"`) || strings.Contains(body, `"revision"`) {
		t.Errorf("failed write returned the uncommitted record: %s", body)
	}
	if title, revision := w.row(id); title != "First draft" || revision != 1 {
		t.Errorf("failed recording changed the row to %q on revision %d", title, revision)
	}
	after := w.versionsOf(id)
	if len(before) != len(after) {
		t.Fatalf("failed recording changed the version count from %d to %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Errorf("failed recording rewrote version %d", before[i].Version)
		}
	}
	if n := w.outbox("memos.memo.updated"); n != 0 {
		t.Errorf("failed recording emitted %d updated events", n)
	}
}
