package rest_test

// This file is the version a mounted resource keeps of every write it commits, at
// the package that writes it.
//
// Three claims, in the order the risk falls. A version is written by the door that
// wrote the row, in that write's own transaction, so a refused write leaves no
// version and an accepted one leaves exactly one — a copy written afterwards would
// be a copy of what the caller asked for and not of what the row holds. The version
// number is the row's own write count, so nothing in the kernel grows a second
// counter that can disagree with the tag a conditional write quotes. And the restore
// is the generated write door called with the stored version's writable fields: it
// rechecks the revision the caller quoted inside its own transaction, so the race
// against an edit settles as a 412 that writes nothing and publishes nothing, and it
// replays no field the record owns about itself.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// Memo is an entity whose writes are kept. Its Revision is the write count History
// numbers versions with; Status is on the Spec's Immutable list, because the door
// that publishes a memo is not the door that edits it and a restore that replayed it
// would unpublish a page somebody had since released.
type Memo struct {
	crud.Base
	Title    string `json:"title" validate:"required"`
	Body     string `json:"body,omitempty" gorm:"type:text"`
	Status   string `json:"status,omitempty"`
	Revision int64  `json:"revision" gorm:"not null;default:1" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`
}

func (Memo) TableName() string { return "rest_memos" }

// Validate normalises on the way in, which is what makes the second case below mean
// something: the version has to hold the trimmed value the row holds, not the padded
// one the caller sent.
func (m *Memo) Validate(context.Context) error {
	m.Title = strings.TrimSpace(m.Title)
	if m.Title == "" {
		return errors.New("a memo needs a title")
	}
	return nil
}

const memoDDL = `
CREATE TABLE rest_memos (
	id uuid PRIMARY KEY,
	tenant_id uuid NOT NULL,
	created_at timestamptz NOT NULL DEFAULT now(),
	updated_at timestamptz NOT NULL DEFAULT now(),
	deleted_at timestamptz,
	title text NOT NULL,
	body text NOT NULL DEFAULT '',
	status text NOT NULL DEFAULT 'draft',
	revision bigint NOT NULL DEFAULT 1
);
ALTER TABLE rest_memos ENABLE ROW LEVEL SECURITY;
ALTER TABLE rest_memos FORCE ROW LEVEL SECURITY;
CREATE POLICY rest_memos_tenant ON rest_memos
	USING (platformkit_tenant_match(tenant_id))
	WITH CHECK (platformkit_tenant_match(tenant_id));`

// keptVersions is the store kit/rest records through: the writes it was handed, and
// the version a restore reads back. It answers from a slice because the claims here
// are about when the kit calls the port and with what — the store's own behaviour is
// tested at modules/history, against the table it owns.
type keptVersions struct {
	kept []httpx.VersionInput
	// inTxn records whether the port was reached inside a live transaction, which is
	// the difference between a version committed with its write and one appended to
	// the database afterwards.
	inTxn []bool
}

func (k *keptVersions) RecordVersion(ctx context.Context, tx db.Tx[db.Tenant], in httpx.VersionInput) (uuid.UUID, error) {
	var live bool
	if err := tx.DB().WithContext(ctx).Raw("SELECT 1").Scan(&live).Error; err == nil {
		live = true
	}
	k.inTxn = append(k.inTxn, live)
	k.kept = append(k.kept, in)
	return uuid.New(), nil
}

func (k *keptVersions) ListVersions(_ context.Context, _ db.Tx[db.Tenant], q httpx.VersionQuery) ([]httpx.Version, error) {
	var out []httpx.Version
	for _, in := range k.kept {
		if in.Module != q.Module || in.Entity != q.Entity || in.Record != q.Record || in.Locale != q.Locale {
			continue
		}
		if q.Before > 0 && in.Version >= q.Before {
			continue
		}
		out = append([]httpx.Version{versionOf(in)}, out...)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (k *keptVersions) Version(_ context.Context, _ db.Tx[db.Tenant], key httpx.VersionKey) (httpx.Version, error) {
	for _, in := range k.kept {
		if in.Module == key.Module && in.Entity == key.Entity && in.Record == key.Record &&
			in.Locale == key.Locale && in.Version == key.Version {
			return versionOf(in), nil
		}
	}
	return httpx.Version{}, crud.ErrNotFound
}

func versionOf(in httpx.VersionInput) httpx.Version {
	return httpx.Version{
		ID: uuid.New(), Module: in.Module, Entity: in.Entity, Record: in.Record, Locale: in.Locale,
		Version: in.Version, Fields: in.Fields, Changed: in.Changed, Reason: in.Reason,
		Actor: in.Actor, At: in.At,
	}
}

// memosWorld is the mounted Spec, the versions it recorded, and the row the cases
// below write on.
type memosWorld struct {
	router http.Handler
	admin  *sql.DB
	kept   *keptVersions
	memo   uuid.UUID
}

// memos mounts the versioned Spec over the store the case reads afterwards.
func memos(t *testing.T) memosWorld {
	t.Helper()
	kept := &keptVersions{}
	s := rest.Spec[*Memo]{
		Module: "memos", Entity: "memo", Path: "/memo",
		Read: "memo:read", Write: "memo:write",
		History: true, Versions: kept, Immutable: []string{"status"},
	}
	api, router, admin := mountAs(t, s, caller{})
	if _, err := admin.ExecContext(t.Context(), memoDDL); err != nil {
		t.Fatalf("create the versioned table: %v", err)
	}
	_ = api
	code, body := call(t, router, http.MethodPost, "/api/v1/memos/memo", `{"title":"First draft"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST the memo every case asks about = %d %s", code, body)
	}
	return memosWorld{router: router, admin: admin, kept: kept, memo: uuid.MustParse(id(t, body))}
}

func (w memosWorld) address() string { return "/api/v1/memos/memo/" + w.memo.String() }

func (w memosWorld) edit(t *testing.T, title string) (int, string) {
	t.Helper()
	return call(t, w.router, http.MethodPatch, w.address(), `{"title":"`+title+`"}`)
}

func (w memosWorld) restore(t *testing.T, version, expected int64) (int, string) {
	t.Helper()
	return call(t, w.router, http.MethodPost, w.address()+"/restore",
		`{"version":`+strconv.FormatInt(version, 10)+`,"expectedRevision":`+strconv.FormatInt(expected, 10)+`}`)
}

func (w memosWorld) readBack(t *testing.T) (title, status string, revision int64) {
	t.Helper()
	if err := w.admin.QueryRowContext(t.Context(),
		`SELECT title, status, revision FROM rest_memos WHERE id = $1`, w.memo).Scan(&title, &status, &revision); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	return title, status, revision
}

// field is one name out of a stored version's document, which is how the cases below
// ask what a write left without depending on the document's spelling.
func field(t *testing.T, document json.RawMessage, name string) string {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatalf("the version holds %s, which is no document: %v", document, err)
	}
	value, _ := doc[name].(string)
	return value
}

// TestEveryCommittedWriteOfAVersionedResourceKeepsExactlyOneVersion is the record
// the panel reads: one row per write, numbered by the row itself, holding the values
// that write committed rather than the ones the body asked for.
func TestEveryCommittedWriteOfAVersionedResourceKeepsExactlyOneVersion(t *testing.T) {
	w := memos(t)
	if _, body := w.edit(t, "  Second title  "); !strings.Contains(body, "Second title") {
		t.Fatalf("PATCH the second title: %s", body)
	}
	w.edit(t, "Third title")

	if len(w.kept.kept) != 3 {
		t.Fatalf("three writes left %d versions, want three", len(w.kept.kept))
	}
	for i, live := range w.kept.inTxn {
		if !live {
			t.Errorf("version %d was recorded outside the transaction that wrote its row", i+1)
		}
	}
	for i, in := range w.kept.kept {
		if want := int64(i + 1); in.Version != want {
			t.Errorf("version %d is numbered %d, want the row's own write count %d", i+1, in.Version, want)
		}
		if in.Module != "memos" || in.Entity != "memo" || in.Record != w.memo {
			t.Errorf("version %d is addressed as %s/%s %s, want memos/memo %s",
				i+1, in.Module, in.Entity, in.Record, w.memo)
		}
		if in.Locale == "" {
			t.Errorf("version %d names no language, so no series can be read", i+1)
		}
		if in.At.IsZero() {
			t.Errorf("version %d was written at no instant", i+1)
		}
		if in.Actor != principal {
			t.Errorf("version %d names %s as its author, want the signed-in %s", i+1, in.Actor, principal)
		}
	}
	// The document is the row as the UPDATE left it: Validate trims, and the version
	// of the padded write holds the trimmed title.
	if got := field(t, w.kept.kept[1].Fields, "title"); got != "Second title" {
		t.Errorf("version 2 holds the title %q, want the value the write stored, %q", got, "Second title")
	}
	// The first version of a record moved nothing, because nothing preceded it.
	if len(w.kept.kept[0].Changed) != 0 {
		t.Errorf("version 1 reports the fields %v, want none: a create has no predecessor", w.kept.kept[0].Changed)
	}
	if got := w.kept.kept[1].Changed; len(got) != 1 || got[0] != "title" {
		t.Errorf("version 2 reports the fields %v, want [title]", w.kept.kept[1].Changed)
	}
	for _, in := range w.kept.kept {
		if in.Reason != "" {
			t.Errorf("an ordinary save recorded the reason %q, want none", in.Reason)
		}
	}
}

// TestARestoreWritesTheStoredVersionBackAsANewOneAndTouchesNoOlderVersion is the
// brief's acceptance: three edits and a restore of the first leaves a fourth version
// holding the first's content, with the second and third untouched — so a person can
// read what they removed as well as get it back.
func TestARestoreWritesTheStoredVersionBackAsANewOneAndTouchesNoOlderVersion(t *testing.T) {
	w := memos(t)
	w.edit(t, "Second title")
	w.edit(t, "Third title")

	code, body := w.restore(t, 1, 3)
	if code != http.StatusOK {
		t.Fatalf("POST restore of version 1 = %d %s", code, body)
	}
	if title, _, revision := w.readBack(t); title != "First draft" || revision != 4 {
		t.Errorf("the restore left title %q on revision %d, want the first title on revision 4", title, revision)
	}
	if len(w.kept.kept) != 4 {
		t.Fatalf("three edits and a restore left %d versions, want four", len(w.kept.kept))
	}
	restored := w.kept.kept[3]
	if restored.Version != 4 {
		t.Errorf("the restored write is numbered %d, want 4", restored.Version)
	}
	if got := field(t, restored.Fields, "title"); got != "First draft" {
		t.Errorf("the restored version holds the title %q, want the first version's", got)
	}
	if restored.Reason != "restore:1" {
		t.Errorf("the restored version records the reason %q, want restore:1", restored.Reason)
	}
	// The versions a restore passes are byte-for-byte what they were.
	for i := range 3 {
		if got := w.kept.kept[i].Version; got != int64(i+1) {
			t.Errorf("version %d is numbered %d after the restore: a restore rewrites history", i+1, got)
		}
	}
	if got := field(t, w.kept.kept[1].Fields, "title"); got != "Second title" {
		t.Errorf("version 2 holds the title %q after the restore, want the second write's", got)
	}
	if n := count(t, w.admin, rest.RestoredEvent); n != 1 {
		t.Errorf("the restore published %d %s rows, want one", n, rest.RestoredEvent)
	}
}

// TestARestoreReplaysNoFieldTheRecordOwns is 0069 §6's boundary with publishing: a
// stored version is the whole row, and the half of it the lifecycle doors own — here
// the status a release moved on — stays where the record put it.
func TestARestoreReplaysNoFieldTheRecordOwns(t *testing.T) {
	w := memos(t)
	if _, err := w.admin.ExecContext(t.Context(),
		`UPDATE rest_memos SET status = 'released' WHERE id = $1`, w.memo); err != nil {
		t.Fatalf("release the memo: %v", err)
	}
	w.edit(t, "Second title")

	code, body := w.restore(t, 1, 2)
	if code != http.StatusOK {
		t.Fatalf("POST restore of version 1 = %d %s", code, body)
	}
	if _, status, _ := w.readBack(t); status != "released" {
		t.Errorf("the restore of a version written while the memo was a draft left status %q, want the release left alone", status)
	}
	// And the write count the version was stored under is not a value a write sets:
	// the row moved to its next number rather than back to the stored one.
	if _, _, revision := w.readBack(t); revision != 3 {
		t.Errorf("the restore left the row on revision %d, want the next one, 3", revision)
	}
}

// TestARestoreAgainstARevisionTheCallerDidNotReadWritesNoVersionAndPublishesNothing
// is rule 9 at the restore's door: the recheck happens inside the transaction, under
// the row lock, and a refusal takes nothing with it — not a version, not an event,
// not the row.
func TestARestoreAgainstARevisionTheCallerDidNotReadWritesNoVersionAndPublishesNothing(t *testing.T) {
	w := memos(t)
	w.edit(t, "Second title")

	code, body := w.restore(t, 1, 1)
	if code != http.StatusPreconditionFailed {
		t.Fatalf("POST restore quoting revision 1 against a row on 2 = %d %s, want 412", code, body)
	}
	if len(w.kept.kept) != 2 {
		t.Errorf("the refused restore recorded %d versions, want the two the real writes made", len(w.kept.kept))
	}
	if n := count(t, w.admin, rest.RestoredEvent); n != 0 {
		t.Errorf("the refused restore published %d %s rows, want none", n, rest.RestoredEvent)
	}
	if n := count(t, w.admin, "memos.memo.updated"); n != 1 {
		t.Errorf("the refused restore left %d %s rows, want the one the real edit made", n, "memos.memo.updated")
	}
	if title, _, revision := w.readBack(t); title != "Second title" || revision != 2 {
		t.Errorf("the refused restore left title %q on revision %d, want the row as the edit left it", title, revision)
	}
}

// TestAVersionedSpecCannotBeMountedWithoutTheStoreOrTheWriteCountIsRefused names at
// mount the two declarations that make History mean anything. A silently dropped
// version is a document's content nobody can get back, so neither defaults: one is
// refused per line, in the sentence that names the Spec to fix.
func TestAVersionedSpecCannotBeMountedWithoutTheStoreOrTheWriteCountIsRefused(t *testing.T) {
	noStore := refusal(t, func() {
		mountAs(t, rest.Spec[*Memo]{
			Module: "memos", Entity: "memo", Path: "/memo",
			Read: "memo:read", Write: "memo:write", History: true,
		}, caller{})
	})
	if !strings.Contains(noStore, "names no store to keep them in") {
		t.Errorf("a History Spec with no store refused with %q, want the sentence that names the missing store", noStore)
	}

	noWriteCount := refusal(t, func() {
		mountAs(t, rest.Spec[*Note]{
			Module: "notes", Entity: "note", Path: "/note",
			Read: "note:read", Write: "note:write", History: true, Versions: &keptVersions{},
		}, caller{})
	})
	if !strings.Contains(noWriteCount, "write count") {
		t.Errorf("a History Spec whose entity carries no revision refused with %q, want the missing write count named", noWriteCount)
	}
}

// numbered is the number one entry of a versions page carries.
func numbered(item map[string]any) int64 {
	for _, name := range []string{"version", "Version"} {
		if n, ok := item[name].(float64); ok {
			return int64(n)
		}
	}
	return 0
}

// refusal is the sentence a mount refused with, or "" when it mounted at all: the
// refusals here are panics, because a Spec that cannot keep what it declared is a
// program defect and no application should ever boot past it.
func refusal(t *testing.T, mount func()) string {
	t.Helper()
	var got any
	func() {
		defer func() { got = recover() }()
		mount()
	}()
	if got == nil {
		return ""
	}
	if msg, ok := got.(string); ok {
		return msg
	}
	return fmt.Sprint(got)
}

// TestAVersionedResourceReadsItsSeriesAtItsOwnAddress is the panel's read mounted
// where the record lives: newest first, one locale, and the version the earlier page
// starts below. It is guarded by the record's own read permission, which is why this
// module mounts no page of its own.
func TestAVersionedResourceReadsItsSeriesAtItsOwnAddress(t *testing.T) {
	w := memos(t)
	w.edit(t, "Second title")
	w.edit(t, "Third title")

	code, body := call(t, w.router, http.MethodGet, w.address()+"/versions?limit=2", "")
	if code != http.StatusOK {
		t.Fatalf("GET the versions of a memo = %d %s", code, body)
	}
	// Read out of the answer rather than into a shape of this case's own: the field
	// spelling is httpx's, and what this case checks is which versions are on the
	// page, in what order, and what it offers for the page before it.
	var page struct {
		Items []map[string]any `json:"items"`
		Next  float64          `json:"next"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("the versions page is not a page this case can read: %v — %s", err, body)
	}
	if len(page.Items) != 2 {
		t.Fatalf("the page answered %d versions, want the two it was capped at: %s", len(page.Items), body)
	}
	if first, second := numbered(page.Items[0]), numbered(page.Items[1]); first != 3 || second != 2 {
		t.Errorf("the page answered versions %d and %d, want 3 then 2, newest first: %s", first, second, body)
	}
	if page.Next != 2 {
		t.Errorf("the page offers %v as the version the earlier page starts below, want 2, the last one it answered: %s", page.Next, body)
	}
}
