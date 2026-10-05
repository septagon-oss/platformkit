package rest_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// Note is an entity that reports what a save replaced: the trail's field, the one
// method, and nothing else. Every module whose Spec publishes an `<entity>.updated`
// event writes these six lines; what the kernel owes them is that the diff is computed
// from the row the write actually locked, and not from whatever the body claimed.
type Note struct {
	crud.Base
	Title  string `json:"title" validate:"required"`
	Body   string `json:"body,omitempty" gorm:"type:text"`
	Colour string `json:"colour,omitempty"`
	// Changes is filled by the door, not by the module, and cleared before the
	// response is written.
	Changes []events.Change `json:"changes,omitempty" gorm:"-" hidden:"true"`
}

func (Note) TableName() string { return "rest_notes" }

// SetChanges is events.Recorder.
func (n *Note) SetChanges(changes []events.Change) { n.Changes = changes }

func (n *Note) Validate(context.Context) error {
	if strings.TrimSpace(n.Title) == "" {
		return errors.New("a note needs a title")
	}
	return nil
}

const notesDDL = `
CREATE TABLE rest_notes (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	body text NOT NULL DEFAULT '',
	colour text NOT NULL DEFAULT ''
);
ALTER TABLE rest_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_notes FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_notes_tenant ON rest_notes
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

var noteSpec = rest.Spec[*Note]{
	Module: "notes", Entity: "note", Path: "/note",
	Read: "note:read", Write: "note:write",
}

// mountNotes is mountAs for the entity that reports its diff. The Spec, the table and
// the chain are this case's own, so nothing here depends on how the plain Task fixture
// above happens to be written.
func mountNotes(t *testing.T) (chi.Router, *sql.DB) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), notesDDL); err != nil {
		t.Fatalf("create notes: %v", err)
	}
	loader := httpx.TenantLoader(caller{})
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: loader, Conn: app, Authorize: caller{},
		Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	noteSpec.Mount(api.Surfaces(noteSpec.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router, admin
}

// TestASaveReportsWhatItReplaced is the reason the field exists: the kernel holds the
// locked before-image of a PATCH it applied, so it can say what the save replaced, and
// the trail row for the save carries it. The response body is the row, and the diff is
// not part of it.
func TestASaveReportsWhatItReplaced(t *testing.T) {
	router, admin := mountNotes(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/notes/note", `{"title":"first","colour":"red"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/notes/note/" + id(t, body)
	if code, body := call(t, router, http.MethodPatch, at, `{"colour":"blue"}`); code != http.StatusOK {
		t.Fatalf(`PATCH {"colour":"blue"} = %d %s`, code, body)
	} else if strings.Contains(body, `"changes"`) {
		t.Errorf("the response body carries the diff, which belongs to the event: %s", body)
	}

	payload := payloadOf(t, admin, noteSpec.Event(rest.Updated))
	var out struct {
		Title   string `json:"title"`
		Colour  string `json:"colour"`
		Changes []struct {
			Field  string          `json:"field"`
			Before json.RawMessage `json:"before"`
			After  json.RawMessage `json:"after"`
		} `json:"changes"`
	}
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatalf("the payload is not json: %v (%s)", err, payload)
	}
	if out.Colour != "blue" || out.Title != "first" {
		t.Errorf("the payload describes the row as %q/%q, want blue/first", out.Colour, out.Title)
	}
	if len(out.Changes) != 1 {
		t.Fatalf("the payload carries %d changes, want the one the PATCH named: %s", len(out.Changes), payload)
	}
	c := out.Changes[0]
	if c.Field != "colour" || string(c.Before) != `"red"` || string(c.After) != `"blue"` {
		t.Errorf("the change is %s from %s to %s, want colour from \"red\" to \"blue\"", c.Field, c.Before, c.After)
	}
}

// TestASaveThatReportsNothingSaysNothing: a save the caller sent no column for
// publishes no event at all, and one that named a column whose value the row already
// held publishes the row and no change. A trail that recorded "colour went from red to
// red" would be a trail that lies once per no-op save.
func TestASaveThatReportsNothingSaysNothing(t *testing.T) {
	router, admin := mountNotes(t)
	code, body := call(t, router, http.MethodPost, "/api/v1/notes/note", `{"title":"steady","colour":"red"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	at := "/api/v1/notes/note/" + id(t, body)
	if code, body := call(t, router, http.MethodPatch, at, `{"colour":"red"}`); code != http.StatusOK {
		t.Fatalf(`PATCH {"colour":"red"} = %d %s`, code, body)
	}
	if n := count(t, admin, noteSpec.Event(rest.Created)); n != 1 {
		t.Fatalf("the create published %d events, want one", n)
	}
	if payload := payloadOf(t, admin, noteSpec.Event(rest.Updated)); strings.Contains(payload, `"changes"`) {
		t.Errorf("a save that moved nothing carried a diff: %s", payload)
	}
}

// payloadOf is the payload the door last stamped for an event, as the outbox holds it.
func payloadOf(t *testing.T, admin *sql.DB, name string) string {
	t.Helper()
	var payload string
	err := admin.QueryRowContext(t.Context(),
		`SELECT payload::text FROM platformkit_outbox WHERE name = $1 ORDER BY created_at DESC, id DESC LIMIT 1`,
		name).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("nothing was ever stamped published for %s", name)
	}
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return payload
}
