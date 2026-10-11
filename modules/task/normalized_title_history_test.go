package task_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/task"
)

func mountedTaskTrail(t *testing.T) (*sql.DB, *db.Conn, http.Handler, events.Transport) {
	t.Helper()
	admin, conn := dbtest.Schema(t, task.Migrations, audit.Migrations)
	mods := module.Expand([]module.Module{task.New(task.Deps{}), audit.New(audit.Deps{})})
	transport := memory.New()
	if err := events.Consume(t.Context(), conn, transport, mods[1].Subscriptions); err != nil {
		t.Fatal(err)
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: caller{}, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	mods[0].Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return admin, conn, router, transport
}

func TestTaskHistoryRecordsTheNormalizedTitle(t *testing.T) {
	admin, conn, router, transport := mountedTaskTrail(t)
	code, body := call(t, router, http.MethodPost, path, `{"title":"first"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %s", code, body)
	}
	rowID := id(t, body)
	code, body = call(t, router, http.MethodPatch, path+"/"+rowID, `{"title":"  second  "}`)
	if code != http.StatusOK {
		t.Fatalf("update: %d %s", code, body)
	}
	if err := events.Relay(t.Context(), conn, transport); err != nil {
		t.Fatal(err)
	}
	var stored, payload string
	if err := admin.QueryRowContext(t.Context(), "SELECT title FROM tasks WHERE id=$1", rowID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "second" {
		t.Fatalf("stored title = %q, want normalized title", stored)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT payload::text FROM audit_events WHERE name='task.task.updated'").Scan(&payload); err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Title   string          `json:"title"`
		Changes []events.Change `json:"changes"`
	}
	if err := json.Unmarshal([]byte(payload), &recorded); err != nil {
		t.Fatal(err)
	}
	if len(recorded.Changes) != 1 {
		t.Fatalf("want one title change: %s", payload)
	}
	c := recorded.Changes[0]
	var after string
	if err := json.Unmarshal(c.After, &after); err != nil {
		t.Fatal(err)
	}
	if c.Field != "title" || string(c.Before) != `"first"` || after != stored || recorded.Title != stored {
		t.Errorf("trail describes title %s -> %s; database and payload title are %q/%q", c.Before, c.After, stored, recorded.Title)
	}
}
