package rest_test

// catalog_writable_narrowing_test.go pins the order inside screens.Describe.
//
// `writable` on a catalogue entry is narrowed by the operation set: a resource
// that mounted no create, no patch and no delete answers writable:false whatever
// its caller's permission says, because nothing here would take the write
// (ui/screens/catalog.go, Describe1). The one thing that still makes such a
// resource genuinely writable is a command the caller may call — the write the
// five verbs do not name.
//
// That predicate reads r.Commands, and Describe narrows r.Commands to the ones
// this caller may call — CommandsFor — before it computes the entry. The whole
// correctness of the narrowed flag therefore rests on which of those two
// statements runs first: ask Describe1 before the narrowing and an entry
// promises a write on the strength of a command this caller would be refused at
// the route, which is the same class of lie `writable` was narrowed to stop.
// Nothing else in the tree holds that order: Describe1 is pure, so the cases
// written beside it pass a flag and a raw resource, and the mounted case that
// exists signs in a caller holding no grant at all, for whom the write guard is
// already false. This case is the composition where the two answers differ: the
// caller holds the Spec's write permission and may not hold the command's.

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestACallerWhoMayNotCallTheOnlyCommandIsNotToldTheResourceIsWritable(t *testing.T) {
	// A resource that offers list and read only, guarded at the door by
	// membership rather than a permission, with one command guarded by a
	// permission this caller does not hold.
	shared := spec
	shared.Operations = []httpx.CRUD{rest.List, rest.Read}
	shared.Read, shared.ReadAuth = "", httpx.SignedIn()
	api, router, _ := mountAs(t, shared, member{"task:write": true})
	rest.Command(api.Surfaces(shared.Module), shared, "void",
		"Void a task", "A door this caller may not open.", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) {
			return nil, nil
		}, rest.CommandOptions{Auth: httpx.Permission("task:void")})

	var out screens.Catalog
	httpx.Register(api.Surfaces(shared.Module).App, huma.Operation{
		OperationID: "probe", Method: http.MethodPost, Path: "/probe", Hidden: true,
		DefaultStatus: http.StatusNoContent,
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		out = screens.Describe(ctx, api.Resources())
		return nil, nil
	})
	if code, body := call(t, router, http.MethodPost, api.Surfaces(shared.Module).App.Path("/probe"), ""); code != http.StatusNoContent {
		t.Fatalf("the probe = %d %s", code, body)
	}
	if len(out.Resources) != 1 {
		t.Fatalf("the catalogue holds %d entries, want the one resource httpx.SignedIn() admits: %+v", len(out.Resources), out.Resources)
	}
	e := out.Resources[0]

	// The caller holds task:write, so the guard alone says yes; the verbs say
	// nothing here answers a write; and the command that would is out of reach.
	if len(e.Commands) != 0 {
		t.Fatalf("the entry publishes the commands %v, want none for a caller who does not hold task:void", e.Commands)
	}
	if e.Writable {
		t.Errorf("operations %v name no write verb and the only command is one this caller may not call, yet the entry says writable=true", e.Operations)
	}
	if e.WritePath != "" {
		t.Errorf("a non-writable entry still addresses writes at %q", e.WritePath)
	}
}
