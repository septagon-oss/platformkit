package rest_test

// This file is the command door at the package that owns it.
//
// The applications' own suite shows one product's answer at one door; what is
// under test here is what kit/rest does with an answer, which no application-level
// case can separate from that application's vocabulary. Three claims, each of
// which a moved line inside `Command` would break silently:
//
//   - a command on a gated Spec is asked about, whatever it changed, and it is
//     asked before its own closure runs (a door that asked afterwards would refuse
//     a write it had already performed);
//   - the question names the command's verb and no field list, because the kernel
//     cannot know what a command writes without running it — so the module that
//     owns the row is the one that can answer, and a list copied onto the command
//     would be a fact the author could quietly forget to write down;
//   - a refusal at that door leaves the row, its revision and the outbox exactly
//     as they were, which is rule 9 at a door the request body never described.

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// gateDoc is the entity behind a door: the revision a gated Spec has to carry, and
// one field, so that a refusal can only ever be about the write and not about a
// second candidate field.
type gateDoc struct {
	crud.Base
	Title    string `json:"title" validate:"required"`
	Revision int64  `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`
}

func (gateDoc) TableName() string { return "rest_gate_docs" }

const gateDDL = `
CREATE TABLE rest_gate_docs (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	revision bigint NOT NULL DEFAULT 1
);
ALTER TABLE rest_gate_docs ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_gate_docs FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_gate_docs_tenant ON rest_gate_docs
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// recorder is a door with two knobs: every write it was asked about, in order, and
// the error it answers with.
type recorder struct {
	seen []rest.Write
	err  error
}

func (r *recorder) Check(_ context.Context, _ db.Tx[db.Tenant], w rest.Write) error {
	r.seen = append(r.seen, w)
	return r.err
}

// gatedWorld is the mounted Spec, its door, the row every case starts from, and how
// many times the command's own closure ran.
type gatedWorld struct {
	router http.Handler
	admin  *sql.DB
	door   *recorder
	ran    *int
	doc    uuid.UUID
}

// gated mounts the Spec and the one command over the same row, then creates the row
// every case asks about. `refusal` is what the door answers, and nil means it lets
// the write through: the two cases differ in that one argument and in nothing else.
func gated(t *testing.T, refusal error) gatedWorld {
	t.Helper()
	door := &recorder{err: refusal}
	ran := new(int)
	s := rest.Spec[*gateDoc]{
		Module: "docs", Entity: "doc", Path: "/doc",
		Read: "docs:read", Write: "docs:write", Gate: door,
	}
	api, router, admin := mountAs(t, s, caller{})
	if _, err := admin.ExecContext(t.Context(), gateDDL); err != nil {
		t.Fatalf("create the gated table: %v", err)
	}
	rest.Command(api.Surfaces(s.Module), s, "publish", "Publish a document",
		"Tells everybody it is ready. Repeating it changes nothing.",
		[]string{"docs.doc.published"},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*gateDoc, error) {
			*ran++
			doc, err := crud.GetForUpdate[*gateDoc](tx, id)
			if err != nil {
				return nil, err
			}
			doc.Title = doc.Title + " (published)"
			doc.Revision++
			if err := crud.Update(ctx, tx, doc, "title", "revision", "updated_at"); err != nil {
				return nil, err
			}
			return doc, events.Publish(ctx, tx, "docs.doc.published", doc)
		}, rest.CommandOptions{})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/docs/doc", `{"title":"draft"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the row every case asks about = %d %s", code, body)
	}
	return gatedWorld{router: router, admin: admin, door: door, ran: ran, doc: uuid.MustParse(id(t, body))}
}

// ask is the command's route, and the answer a caller gets.
func (w gatedWorld) ask(t *testing.T) (int, string) {
	t.Helper()
	return call(t, w.router, http.MethodPost, "/api/v1/docs/doc/"+w.doc.String()+"/publish", "")
}

// readBack is the row as the database holds it, which is the only witness a refused
// write can be checked against.
func (w gatedWorld) readBack(t *testing.T) (string, int64) {
	t.Helper()
	var title string
	var revision int64
	if err := w.admin.QueryRowContext(t.Context(),
		`SELECT title, revision FROM rest_gate_docs WHERE id = $1`, w.doc).Scan(&title, &revision); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	return title, revision
}

// TestACommandIsAskedAtItsOwnDoorBeforeItRuns pins the question the module gets.
func TestACommandIsAskedAtItsOwnDoorBeforeItRuns(t *testing.T) {
	w := gated(t, nil)

	// Creating the row went through this same Spec, and the door never moved: a
	// proposal is a diff over a row measured against a revision, so there is no
	// proposal about a row that does not exist yet, and a gate on create would be
	// the only way to make an entity uncreatable while its switch was on.
	if len(w.door.seen) != 0 {
		t.Fatalf("the door was asked about the create: %+v", w.door.seen)
	}

	code, body := w.ask(t)
	if code != http.StatusOK {
		t.Fatalf("POST publish = %d %s, want 200", code, body)
	}
	if len(w.door.seen) != 1 {
		t.Fatalf("the door was asked %d times about one command, want once: %+v", len(w.door.seen), w.door.seen)
	}
	got := w.door.seen[0]
	if got.Verb != rest.VerbCommand || got.Command != "publish" {
		t.Errorf("the door was asked about verb %q command %q, want %q %q",
			got.Verb, got.Command, rest.VerbCommand, "publish")
	}
	if got.Module != "docs" || got.Entity != "doc" || got.ID != w.doc {
		t.Errorf("the door was asked about %s.%s/%s, want docs/doc/%s — the subject comes from the Spec, never the request",
			got.Module, got.Entity, got.ID, w.doc)
	}
	if len(got.Changed) != 0 {
		t.Errorf("the door was handed the field list %v for a command, want none: only the module that owns the row knows what its commands move",
			got.Changed)
	}
	if title, revision := w.readBack(t); title != "draft (published)" || revision != 2 {
		t.Errorf("the allowed command left title %q revision %d, want %q and %d",
			title, revision, "draft (published)", 2)
	}
	if n := count(t, w.admin, "docs.doc.published"); n != 1 {
		t.Errorf("the allowed command published %d docs.doc.published rows, want one", n)
	}
}

// TestACommandRefusedAtItsOwnDoorWritesNothing is rule 9 at the one door a request
// body never described: the refusal is not about a field somebody sent, so nothing
// about the row can be trusted to say what the write would have done.
func TestACommandRefusedAtItsOwnDoorWritesNothing(t *testing.T) {
	w := gated(t, fmt.Errorf("%w: this document is published by proposal", crud.ErrConflict))

	if n := count(t, w.admin, "docs.doc.published"); n != 0 {
		t.Fatalf("the fixture published %d rows before anything was refused", n)
	}
	code, body := w.ask(t)
	if code != http.StatusConflict {
		t.Fatalf("POST publish under a refusing door = %d %s, want 409", code, body)
	}
	if *w.ran != 0 {
		t.Errorf("the command's own closure ran %d times behind a door that refused it", *w.ran)
	}
	if title, revision := w.readBack(t); title != "draft" || revision != 1 {
		t.Errorf("the refused command left title %q revision %d, want the row as it was: %q and %d",
			title, revision, "draft", 1)
	}
	if n := count(t, w.admin, "docs.doc.published"); n != 0 {
		t.Errorf("the refused command put %d docs.doc.published rows in the outbox, want none", n)
	}
	if body == "" {
		t.Error("the refusal reached the caller as nothing at all")
	}
}
