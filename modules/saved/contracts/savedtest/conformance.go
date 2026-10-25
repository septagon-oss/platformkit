package savedtest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	picture "github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
)

// Source is an explicit fixture collaborator, shared by memory and SQL worlds.
// Failure returns zero; OnCapture can revoke a grant during a call. Configured
// values are returned verbatim so saved must check even an incorrect version.
type Source struct {
	Value       contracts.Capture
	Failure     error
	OnCapture   func()
	Destination string
}

func (s *Source) Capture(_ context.Context, _ db.Tx[db.Tenant], _, _ string) (contracts.Capture, error) {
	if s.OnCapture != nil {
		s.OnCapture()
	}
	if s.Failure != nil {
		return contracts.Capture{}, s.Failure
	}
	return s.Value, nil
}

func (s *Source) Check(context.Context, db.Tx[db.Tenant], string) error { return s.Failure }

func (s *Source) Open(context.Context, db.Tx[db.Tenant], string) (string, error) {
	if s.Failure != nil {
		return "", s.Failure
	}
	return s.Destination, nil
}

// Fixture supplies a tenant transaction per call, including calls made under a
// different tenant. Rows witnesses all fixture tenants, not just the visitor.
type Fixture struct {
	Owner       context.Context
	OtherPerson context.Context
	OtherTenant context.Context
	Denied      context.Context
	Service     contracts.Service
	// Undeclared is the same adapter over the same store with no source
	// declaration left in it: this composition one release later, still holding
	// the copies of a source it no longer names. A world that cannot offer it
	// cannot show that a copy outlives what it was taken from.
	Undeclared contracts.Service
	Within     func(context.Context, func(db.Tx[db.Tenant]) error) error
	Rows       func() string
	Published  func() []string
	Source     *Source
	Clock      *porttest.Clock
	Revoke     func()
	// Picture is the composition the retention case saves, and ComposedPicture
	// the exact document the compositor makes of it. A world brings both from the
	// picture module's own fixture, which only test source may import.
	Picture         contracts.PictureInput
	ComposedPicture string
}

type Harness func(*testing.T, func(Fixture))

var first = contracts.Ref{Module: "example", Kind: "thing", Key: "one"}

func save(f Fixture, ctx context.Context, ref contracts.Ref) (contracts.Item, error) {
	var out contracts.Item
	err := f.Within(ctx, func(tx db.Tx[db.Tenant]) error {
		var err error
		out, err = f.Service.Save(ctx, tx, contracts.Save{Source: ref, ExpectedSourceVersion: "v1"})
		return err
	})
	return out, err
}

func unsave(f Fixture, ctx context.Context, id uuid.UUID, revision int64) error {
	return f.Within(ctx, func(tx db.Tx[db.Tenant]) error {
		return f.Service.Unsave(ctx, tx, contracts.Unsave{ID: id.String(), ExpectedRevision: revision})
	})
}

func get(f Fixture, ctx context.Context, id uuid.UUID) (contracts.View, error) {
	var out contracts.View
	err := f.Within(ctx, func(tx db.Tx[db.Tenant]) error {
		var err error
		out, err = f.Service.Get(ctx, tx, id.String())
		return err
	})
	return out, err
}

func list(f Fixture, ctx context.Context, q contracts.Query) (contracts.Page, error) {
	var out contracts.Page
	err := f.Within(ctx, func(tx db.Tx[db.Tenant]) error {
		var err error
		out, err = f.Service.List(ctx, tx, q)
		return err
	})
	return out, err
}

func mustSave(t *testing.T, f Fixture, ref contracts.Ref) contracts.Item {
	t.Helper()
	row, err := save(f, f.Owner, ref)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func render(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

func classify(err error) porttest.Class {
	if errors.Is(err, contracts.ErrNotFound) || errors.Is(err, contracts.ErrUnavailable) {
		return porttest.Immutable
	}
	return porttest.Correctable
}

func refusedSave(f Fixture, ctx context.Context) error {
	out, err := save(f, ctx, first)
	if !reflect.DeepEqual(out, contracts.Item{}) {
		return fmt.Errorf("refused save returned data: %v", out.ID)
	}
	return err
}

func snapshot(_ *testing.T, f Fixture, id uuid.UUID) string {
	if id == uuid.Nil {
		return f.Rows()
	}
	row, _ := get(f, f.Owner, id)
	return render(struct {
		All string
		Row contracts.View
	}{f.Rows(), row})
}

// RunService runs the same contract against any adapter. Database-only atomicity
// and concurrency cases are additional obligations, specified in the module README.
func RunService(t *testing.T, h Harness) { t.Helper(); porttest.Run(t, Suite(h)) }

func Suite(h Harness) porttest.Suite[Fixture] {
	return porttest.Suite[Fixture]{Port: "saved.Service", World: h,
		Events: func(f Fixture) []string { return f.Published() }, Classify: classify,
		Ops: []porttest.Op[Fixture]{
			{Name: "Save", Mutates: true, Ready: func(*testing.T, Fixture) uuid.UUID { return uuid.Nil },
				Call: func(f Fixture, _ uuid.UUID) (string, error) {
					row, err := save(f, f.Owner, first)
					return render(row), err
				},
				Snapshot: snapshot, Publishes: []string{contracts.EventSaved},
				Skip: map[porttest.Kind]string{porttest.Elsewhere: "Save creates a separate personal copy; isolation is checked by the owner/tenant cases below."},
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Class: porttest.Immutable,
						Provoke: func(_ *testing.T, f Fixture, id uuid.UUID) uuid.UUID {
							f.Source.Failure = contracts.ErrUnavailable
							return id
						},
						Call: func(f Fixture, _ uuid.UUID) error { return refusedSave(f, f.Owner) }, Is: is(contracts.ErrUnavailable)},
					{Kind: porttest.Denied, Class: porttest.Correctable,
						Call: func(f Fixture, _ uuid.UUID) error { return refusedSave(f, f.Denied) }, Is: is(contracts.ErrDenied)},
					{Kind: porttest.Stale, Class: porttest.Correctable,
						Provoke: func(_ *testing.T, f Fixture, id uuid.UUID) uuid.UUID { f.Source.Value.Version = "v2"; return id },
						Call:    func(f Fixture, _ uuid.UUID) error { return refusedSave(f, f.Owner) }, Is: is(contracts.ErrStale)},
				}},
			{Name: "Unsave", Mutates: true, Ready: func(t *testing.T, f Fixture) uuid.UUID { return mustSave(t, f, first).ID },
				Call:     func(f Fixture, id uuid.UUID) (string, error) { return "absent", unsave(f, f.Owner, id, 1) },
				Snapshot: snapshot, Publishes: []string{contracts.EventUnsaved},
				Skip: map[porttest.Kind]string{
					porttest.Unknown:   "Absence already satisfies removal; the missing/foreign removal cases assert successful silence.",
					porttest.Elsewhere: "Foreign rows are absent to removal; the owner/tenant cases verify their preservation."},
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Denied, Class: porttest.Correctable, Call: func(f Fixture, id uuid.UUID) error { return unsave(f, f.Denied, id, 1) }, Is: is(contracts.ErrDenied)},
					{Kind: porttest.Stale, Class: porttest.Correctable, Call: func(f Fixture, id uuid.UUID) error { return unsave(f, f.Owner, id, 2) }, Is: is(contracts.ErrStale)},
				}},
		}, Own: cases(),
	}
}

func is(want error) func(Fixture, error) bool {
	return func(_ Fixture, err error) bool { return errors.Is(err, want) }
}

func cases() []porttest.Case[Fixture] {
	out := copyLifetimeCases()
	out = append(out, sourceCallCases()...)
	out = append(out, retainedReadCases()...)
	out = append(out, privateScopeCases()...)
	out = append(out, sourceLossCases()...)
	out = append(out, droppedDeclarationCases()...)
	return append(out, openDestinationCases()...)
}

// The first save, the duplicate that must not refresh it, and the removal that ends it.
func copyLifetimeCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{Name: "the first snapshot and timestamp survive a later source version", Because: "Success content and a sequence are domain assertions.", Run: func(t *testing.T, f Fixture) {
			first := mustSave(t, f, first)
			f.Source.Value.Title, f.Source.Value.Version = "Changed", "v2"
			f.Clock.Advance(time.Hour)
			var again contracts.Item
			err := f.Within(f.Owner, func(tx db.Tx[db.Tenant]) error {
				var err error
				again, err = f.Service.Save(f.Owner, tx, contracts.Save{Source: first.Source, ExpectedSourceVersion: "v2"})
				return err
			})
			if err != nil || !reflect.DeepEqual(first, again) || len(f.Published()) != 1 {
				t.Fatalf("snapshot refreshed: %v, %v", again, err)
			}
		}},
		{Name: "unsave removes the last item and saving again gets another identity", Because: "Three commands distinguish a copy lifetime from a retry.", Run: func(t *testing.T, f Fixture) {
			row := mustSave(t, f, first)
			if err := unsave(f, f.Owner, row.ID, row.Revision); err != nil {
				t.Fatal(err)
			}
			page, err := list(f, f.Owner, contracts.Query{})
			if err != nil || page.Total != 0 || len(page.Items) != 0 {
				t.Fatalf("last removal: %v, %v", page, err)
			}
			next := mustSave(t, f, first)
			if next.ID == row.ID {
				t.Fatal("removed identity reused")
			}
			if err := unsave(f, f.Owner, row.ID, row.Revision); err != nil {
				t.Fatal(err)
			}
			if _, err := get(f, f.Owner, next.ID); err != nil {
				t.Fatal("old removal touched the new copy")
			}
		}},
		{Name: "missing removal is silent even with a stale revision", Because: "An unknown row is success, not the harness's refusal.", Run: func(t *testing.T, f Fixture) {
			before := f.Rows()
			if err := unsave(f, f.Owner, uuid.New(), -1); err != nil {
				t.Fatal(err)
			}
			if f.Rows() != before || len(f.Published()) != 0 {
				t.Fatal("absence wrote or emitted")
			}
		}},
		{Name: "kind filtering precedes total and pagination", Because: "Ordered multi-row reads are not a single-row operation.", Run: func(t *testing.T, f Fixture) {
			mustSave(t, f, first)
			f.Clock.Advance(time.Second)
			wanted := mustSave(t, f, contracts.Ref{Module: "example", Kind: "thing", Key: "two"})
			f.Clock.Advance(time.Second)
			mustSave(t, f, contracts.Ref{Module: "example", Kind: "plan", Key: "three"})
			page, err := list(f, f.Owner, contracts.Query{Kind: "thing", Limit: 1})
			if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].ID != wanted.ID {
				t.Fatalf("filtered page: %v, %v", page, err)
			}
		}},
		{Name: "person filtering precedes total and pagination", Because: "A later foreign row must not consume the owner's page slot.", Run: func(t *testing.T, f Fixture) {
			wanted := mustSave(t, f, first)
			f.Clock.Advance(time.Second)
			if _, err := save(f, f.OtherPerson, first); err != nil {
				t.Fatal(err)
			}
			page, err := list(f, f.Owner, contracts.Query{Limit: 1})
			if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != wanted.ID {
				t.Fatalf("owner page: %v, %v", page, err)
			}
		}},
	}
}

// What the source call may and may not do to a copy: authorization read afresh, and malformed capture refused.
func sourceCallCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{Name: "a grant revoked during capture refuses without data or effects", Because: "The source call is an explicit authorization wait boundary.", Run: func(t *testing.T, f Fixture) {
			f.Source.OnCapture = f.Revoke
			before := f.Rows()
			if err := refusedSave(f, f.Owner); !errors.Is(err, contracts.ErrDenied) {
				t.Fatalf("grant recheck: %v", err)
			}
			if f.Rows() != before || len(f.Published()) != 0 {
				t.Fatal("refusal had effects")
			}
		}},
		{Name: "invalid captured values are correctable and return no row", Because: "Source contract validation requires malformed projection fixtures.", Run: func(t *testing.T, f Fixture) {
			for _, raw := range []string{"", "[]", "null", "{", `{"unterminated":`, "{}" + strings.Repeat(" ", 65535), "{\"x\":\"\xff\"}"} {
				f.Source.Value.Values = json.RawMessage(raw)
				before := f.Rows()
				err := refusedSave(f, f.Owner)
				if !errors.Is(err, contracts.ErrInvalid) || classify(err) != porttest.Correctable || f.Rows() != before || len(f.Published()) != 0 {
					t.Fatalf("invalid capture of %d bytes: %v", len(raw), err)
				}
			}
		}},
		{Name: "reads return detached captured values", Because: "Aliasing across calls cannot be asserted by the operation result alone.", Run: func(t *testing.T, f Fixture) {
			row := mustSave(t, f, first)
			want := string(row.Snapshot.Values)
			row.Snapshot.Values[0] = '!'
			view, err := get(f, f.Owner, row.ID)
			if err != nil || string(view.Snapshot.Values) != want {
				t.Fatalf("shared snapshot: %v", err)
			}
		}},
		{Name: "a saved picture preserves its original composed bytes", Because: "A literal picture oracle distinguishes retention from rerendering current inputs.", Run: func(t *testing.T, f Fixture) {
			f.Source.Value.Picture = f.Picture
			row := mustSave(t, f, first)
			if row.Snapshot.Picture.SVG != f.ComposedPicture {
				t.Fatalf("unexpected retained picture: %s", row.Snapshot.Picture.SVG)
			}
			f.Source.Value.Picture.Value.Colours[0] = "#000000"
			f.Source.Value.Picture.Template = picture.Template{}
			f.Source.Failure = contracts.ErrUnavailable
			view, err := get(f, f.Owner, row.ID)
			if err != nil || view.Availability != "unavailable" || view.Snapshot.Picture.SVG != f.ComposedPicture {
				t.Fatalf("retained picture changed: %v", err)
			}
		}},
	}
}

// The copy a later read answers with: its own bytes, its own picture, and only its own page.
func retainedReadCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{Name: "two canonical variants remain two independent copies", Because: "Natural identity includes the source's full key, not just its recipe family.", Run: func(t *testing.T, f Fixture) {
			one := mustSave(t, f, contracts.Ref{Module: "example", Kind: "thing", Key: "recipe/red"})
			f.Source.Value.Values = json.RawMessage(`{"resolved":["blue"]}`)
			two := mustSave(t, f, contracts.Ref{Module: "example", Kind: "thing", Key: "recipe/blue"})
			again := mustSave(t, f, one.Source)
			page, err := list(f, f.Owner, contracts.Query{})
			if err != nil || page.Total != 2 || one.ID == two.ID || !reflect.DeepEqual(one, again) || len(f.Published()) != 2 {
				t.Fatalf("variant identity: %v", err)
			}
		}},
		{Name: "invalid pagination returns zero without effects", Because: "List refusal includes its total and rows, not just its error.", Run: func(t *testing.T, f Fixture) {
			mustSave(t, f, first)
			before, events := f.Rows(), slices.Clone(f.Published())
			for _, q := range []contracts.Query{{Limit: -1}, {Limit: 101}, {Offset: -1}, {Kind: "Thing"}, {Module: "*"}} {
				out, err := list(f, f.Owner, q)
				if !errors.Is(err, contracts.ErrInvalid) || !reflect.DeepEqual(out, contracts.Page{}) {
					t.Fatalf("invalid query: %v, %v", out, err)
				}
			}
			if f.Rows() != before || !slices.Equal(events, f.Published()) {
				t.Fatal("read changed state")
			}
		}},
		{Name: "a duplicate save rechecks a grant revoked during capture", Because: "A prior successful result must not bypass current authorization.", Run: func(t *testing.T, f Fixture) {
			mustSave(t, f, first)
			before, events := f.Rows(), slices.Clone(f.Published())
			f.Source.OnCapture = f.Revoke
			if err := refusedSave(f, f.Owner); !errors.Is(err, contracts.ErrDenied) {
				t.Fatalf("duplicate grant recheck: %v", err)
			}
			if f.Rows() != before || !slices.Equal(events, f.Published()) {
				t.Fatal("refusal changed the existing copy")
			}
		}},
	}
}

// Another person and another tenant, each asking the same five reads and one removal.
func privateScopeCases() []porttest.Case[Fixture] {
	var out []porttest.Case[Fixture]
	for _, scope := range []string{"person", "tenant"} {
		out = append(out, porttest.Case[Fixture]{Name: "another " + scope + " sees nothing and cannot remove the owner's copy", Because: "Two authorized scopes and an owner-side witness are required.", Run: func(t *testing.T, f Fixture) {
			row := mustSave(t, f, first)
			ctx := f.OtherPerson
			if scope == "tenant" {
				ctx = f.OtherTenant
			}
			view, err := get(f, ctx, row.ID)
			if !errors.Is(err, contracts.ErrNotFound) || !reflect.DeepEqual(view, contracts.View{}) {
				t.Fatalf("foreign read: %v, %v", view, err)
			}
			page, err := list(f, ctx, contracts.Query{})
			if err != nil || page.Total != 0 || len(page.Items) != 0 {
				t.Fatalf("foreign list: %v, %v", page, err)
			}
			var found contracts.View
			err = f.Within(ctx, func(tx db.Tx[db.Tenant]) error {
				var err error
				found, err = f.Service.Find(ctx, tx, first)
				return err
			})
			if !errors.Is(err, contracts.ErrNotFound) || !reflect.DeepEqual(found, contracts.View{}) {
				t.Fatalf("foreign lookup: %v, %v", found, err)
			}
			var path string
			err = f.Within(ctx, func(tx db.Tx[db.Tenant]) error {
				var err error
				path, err = f.Service.Open(ctx, tx, row.ID.String())
				return err
			})
			if !errors.Is(err, contracts.ErrNotFound) || path != "" {
				t.Fatalf("foreign open: %q, %v", path, err)
			}
			before, events := f.Rows(), slices.Clone(f.Published())
			if err := unsave(f, ctx, row.ID, row.Revision); err != nil {
				t.Fatal(err)
			}
			if before != f.Rows() || !slices.Equal(events, f.Published()) {
				t.Fatal("foreign removal changed owner state")
			}
			if _, err := get(f, f.Owner, row.ID); err != nil {
				t.Fatal(err)
			}
		}})
	}
	return out
}

// A source that is gone, revoked or down: the copy stays, the live action does not.
func sourceLossCases() []porttest.Case[Fixture] {
	var out []porttest.Case[Fixture]
	for _, failure := range []struct {
		name   string
		err    error
		status string
	}{
		{"deleted", contracts.ErrUnavailable, "unavailable"}, {"revoked", contracts.ErrDenied, "unavailable"}, {"temporarily unavailable", contracts.ErrTemporary, "temporary"},
	} {
		out = append(out, porttest.Case[Fixture]{Name: "a " + failure.name + " source leaves a copy and refuses live open and save retry", Because: "Retained display and live authorization have different outcomes.", Run: func(t *testing.T, f Fixture) {
			row := mustSave(t, f, first)
			f.Source.Failure = failure.err
			before, events := f.Rows(), slices.Clone(f.Published())
			view, err := get(f, f.Owner, row.ID)
			if err != nil || view.Availability != failure.status || !reflect.DeepEqual(view.Item, row) {
				t.Fatalf("retained view: %v, %v", view, err)
			}
			page, err := list(f, f.Owner, contracts.Query{})
			if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Availability != failure.status || !reflect.DeepEqual(page.Items[0].Item, row) {
				t.Fatalf("retained list: %v, %v", page, err)
			}
			var path string
			err = f.Within(f.Owner, func(tx db.Tx[db.Tenant]) error {
				var err error
				path, err = f.Service.Open(f.Owner, tx, row.ID.String())
				return err
			})
			if !errors.Is(err, failure.err) || path != "" {
				t.Fatalf("live open: %q, %v", path, err)
			}
			if err = refusedSave(f, f.Owner); !errors.Is(err, failure.err) {
				t.Fatalf("retry: %v", err)
			}
			if f.Rows() != before || !slices.Equal(events, f.Published()) {
				t.Fatal("source loss changed the saved copy")
			}
			if err := unsave(f, f.Owner, row.ID, row.Revision); err != nil {
				t.Fatalf("source failure prevented removal: %v", err)
			}
			if _, err := get(f, f.Owner, row.ID); !errors.Is(err, contracts.ErrNotFound) {
				t.Fatalf("removed copy remains: %v", err)
			}
		}})
	}
	return out
}

// A source the composition no longer declares is the last case, not the first:
// the copies it made stay readable, marked, and removable, and only the live
// action goes with it. Both adapters answer through the shared Availability
// decision, which is what makes this the contract rather than one adapter's own
// answer for a (module, kind) nobody declared.
func droppedDeclarationCases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{Name: "a source the composition dropped leaves the copy readable, marked and removable",
			Because: "An adapter may not decide on its own that an undeclared pair is a failed read.",
			Run: func(t *testing.T, f Fixture) {
				row := mustSave(t, f, first)
				ask := func(body func(db.Tx[db.Tenant]) error) error {
					return f.Within(f.Owner, func(tx db.Tx[db.Tenant]) error { return body(tx) })
				}
				copies, events := f.Rows(), slices.Clone(f.Published())

				var view contracts.View
				if err := ask(func(tx db.Tx[db.Tenant]) error {
					var err error
					view, err = f.Undeclared.Get(f.Owner, tx, row.ID.String())
					return err
				}); err != nil || view.Availability != "unavailable" || !reflect.DeepEqual(view.Item, row) {
					t.Fatalf("a dropped source refused the copy rather than marking it: %v, %v", view, err)
				}
				var page contracts.Page
				if err := ask(func(tx db.Tx[db.Tenant]) error {
					var err error
					page, err = f.Undeclared.List(f.Owner, tx, contracts.Query{})
					return err
				}); err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Availability != "unavailable" {
					t.Fatalf("a dropped source emptied the library: %v, %v", page, err)
				}
				var found contracts.View
				if err := ask(func(tx db.Tx[db.Tenant]) error {
					var err error
					found, err = f.Undeclared.Find(f.Owner, tx, first)
					return err
				}); err != nil || found.ID != row.ID || found.Availability != "unavailable" {
					t.Fatalf("a dropped source hid the pressed state: %v, %v", found, err)
				}
				var path string
				err := ask(func(tx db.Tx[db.Tenant]) error {
					var err error
					path, err = f.Undeclared.Open(f.Owner, tx, row.ID.String())
					return err
				})
				if !errors.Is(err, contracts.ErrUnavailable) || path != "" {
					t.Fatalf("a dropped source opened a live action: %q, %v", path, err)
				}
				if f.Rows() != copies || !slices.Equal(events, f.Published()) {
					t.Fatal("reading a source that had gone wrote or emitted")
				}
				if err := ask(func(tx db.Tx[db.Tenant]) error {
					return f.Undeclared.Unsave(f.Owner, tx, contracts.Unsave{ID: row.ID.String(), ExpectedRevision: row.Revision})
				}); err != nil {
					t.Fatalf("a dropped source prevented removal: %v", err)
				}
				if _, err := get(f, f.Owner, row.ID); !errors.Is(err, contracts.ErrNotFound) {
					t.Fatalf("the copy survived its removal: %v", err)
				}
				if want := append(events, contracts.EventUnsaved); !slices.Equal(want, f.Published()) {
					t.Fatalf("the copy's two commands published %v, want %v", f.Published(), want)
				}
			}},
	}
}

// What a live open answers with, and what it refuses to hand over. Every other
// open assertion here is a refusal, so the branch that accepts a destination —
// and the branch that refuses one that leaves the application — had no witness.
func openDestinationCases() []porttest.Case[Fixture] {
	open := func(f Fixture, id uuid.UUID) (string, error) {
		var path string
		err := f.Within(f.Owner, func(tx db.Tx[db.Tenant]) error {
			var err error
			path, err = f.Service.Open(f.Owner, tx, id.String())
			return err
		})
		return path, err
	}
	return []porttest.Case[Fixture]{
		{Name: "open answers the destination its source names, afresh",
			Because: "A retained copy is a right to display its snapshot, never a stored link.",
			Run: func(t *testing.T, f Fixture) {
				row := mustSave(t, f, first)
				copies, events := f.Rows(), slices.Clone(f.Published())
				if path, err := open(f, row.ID); err != nil || path != f.Source.Destination || path == "" {
					t.Fatalf("open: %q, %v, want the source's own %q", path, err, f.Source.Destination)
				}
				f.Source.Destination = "/app/example/things/another"
				if path, err := open(f, row.ID); err != nil || path != "/app/example/things/another" {
					t.Fatalf("open answered a stale destination: %q, %v", path, err)
				}
				for _, away := range []string{"https://elsewhere.example/x", "https://elsewhere.example/app/things",
					"//elsewhere.example/app/x", "relative/path"} {
					f.Source.Destination = away
					if path, err := open(f, row.ID); !errors.Is(err, contracts.ErrInvalid) || path != "" {
						t.Fatalf("open followed %q: %q, %v", away, path, err)
					}
				}
				if f.Rows() != copies || !slices.Equal(events, f.Published()) {
					t.Fatal("open wrote or published")
				}
			}},
	}
}
