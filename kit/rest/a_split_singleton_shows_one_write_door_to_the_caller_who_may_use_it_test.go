package rest_test

// a_split_singleton_shows_one_write_door_to_the_caller_who_may_use_it_test.go
// asks the two consumers of one field about one mount, together.
//
// `httpx.Resource.WritePath` is read twice: the catalogue a phone reads prints it
// as `write_path` (ui/screens/catalog.go) and `API.methodNotAllowed` quotes it at
// a caller who wrote at the read door (kit/httpx/httpx.go, CodeWriteElsewhere).
// Each half has its own case — one reads the projection off a mounted row, one
// reads the refusal at the wrong door — and none asks whether the two answers are
// the same address of the same mount. A row whose write door moved to the operator
// is where they could part: one answer is composed from the registered resource,
// the other by the router, and one caller holds both.
//
// The catalogue half carries a guard the refusal does not. `write_path` is printed
// only for a caller who may write (screens.DescribeEntry), so one mount answers two
// callers differently, and the answer to the caller who may not write names no
// address at all: a door a caller cannot write at is neither true about their own
// permissions nor theirs to be told. That silence is only honest if the door it
// declines to name really refuses them, which is asked here too.

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

const (
	tenantBoardDoor   = "/api/v1/drawing/board"
	operatorBoardDoor = "/api/v1/ops/drawing/board"
)

// shownWriteDoor is the way out a refusal offers: the address that takes a write
// this address does not.
var shownWriteDoor = regexp.MustCompile(`write it at (/[^"\\\s]+)`)

// drawnBoard is a tenant's one drawing row, the shape a module keeps for a drawing
// the tenant configures once and somebody else may hold the door to.
type drawnBoard struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (drawnBoard) TableName() string { return "rest_shown_door_boards" }

// shownDoor is one mount's answer to one caller: what the catalogue said, and what
// the doors answered.
type shownDoor struct {
	// named is the catalogue's `write_path`; listed says the caller was shown an
	// entry at all (Describe drops what the caller may not read, and an absent
	// entry must not be read as a refused write); writable is whether the entry
	// told that caller the resource is theirs to change.
	named    string
	listed   bool
	writable bool
	// readStatus is the answer at the tenant's own door for the read this mount
	// declares — the reachability probe: the row is served, so everything below is
	// about a door and never about a row nothing holds. refusedStatus is what a
	// write asked there answered, pointed the address a refusal named, and
	// takenStatus what the same write got at that address, or at the tenant's own
	// door where no address was named.
	readStatus, refusedStatus, takenStatus int
	pointed                                string
}

// showDoors mounts a tenant's one drawing row with its write on the operator's
// surface and asks it as one caller — the installation's own when `operator` is
// set, a member of the tenant otherwise: the catalogue entry that caller is served,
// the read, and a write at the tenant's own door and at any door a refusal sends
// that caller to.
func showDoors(t *testing.T, operator bool) shownDoor {
	t.Helper()
	_, app := dbtest.Schema(t) // No table: the row is a closure's, the transaction is the tenant's.
	who := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	if operator {
		who = tenancy.Tenant{ID: uuid.New(), Slug: "ops", Operator: true}
	}
	ask := &operatorReader{tenant: who, allow: true}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: ask, Conn: app,
		Authorize: ask, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	board := rest.Singleton[*drawnBoard]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write", OperatorWrite: true,
		Load: func(context.Context, db.Tx[db.Tenant]) (*drawnBoard, error) {
			return &drawnBoard{Drawing: entity.Sketch{{{0, 0, 0}}}}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *drawnBoard) (*drawnBoard, error) {
			return in, nil
		},
	}
	board.Mount(api.Surfaces(board.Module))
	var out screens.Catalog
	httpx.Register(api.Surfaces(board.Module).App, probeOperation("drawing-catalogue-probe", "/catalogue"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			out = screens.Describe(ctx, api.Resources())
			return nil, nil
		})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted doors do not declare themselves: %v", err)
	}
	const body = `{"drawing":[[[0,0,0],[1000,1000,12]]]}`

	got := shownDoor{}
	if code, answer := call(t, router, http.MethodPost, api.Surfaces(board.Module).App.Path("/catalogue"), ""); code != http.StatusNoContent {
		t.Fatalf("the catalogue door = %d %s: this caller reaches this host's workspace, so what the entry says is about the write", code, head(answer))
	}
	for _, e := range out.Resources {
		if e.Schema.Entity == "board" {
			got.named, got.writable, got.listed = e.WritePath, e.Writable, true
		}
	}
	if !got.listed {
		t.Fatalf("the mount registered no catalogue entry for the drawing board; the caller was shown %d entries", len(out.Resources))
	}

	got.readStatus, _ = call(t, router, http.MethodGet, tenantBoardDoor, "")
	var refusal string
	got.refusedStatus, refusal = call(t, router, http.MethodPut, tenantBoardDoor, body)
	got.takenStatus = got.refusedStatus
	if match := shownWriteDoor.FindStringSubmatch(refusal); match != nil {
		got.pointed = match[1]
		got.takenStatus, _ = call(t, router, http.MethodPut, match[1], body)
	}
	return got
}

// TestASplitSingletonShowsOneWriteDoorToTheCallerWhoMayUseIt holds the catalogue's
// `write_path`, the refusal's `write it at` and the router's own answer to be one
// address of one mount for the caller who holds the write, and holds the catalogue
// to naming no address to the caller who does not — where the doors really refuse
// that caller, so the silence is the same sentence the doors say.
func TestASplitSingletonShowsOneWriteDoorToTheCallerWhoMayUseIt(t *testing.T) {
	operator := showDoors(t, true)
	if operator.readStatus != http.StatusOK {
		t.Fatalf("the installation read the tenant's drawing row at %s and got %d: the row is served, so the entry below is about a door",
			tenantBoardDoor, operator.readStatus)
	}
	if !operator.writable {
		t.Errorf("the catalogue told the caller who holds the write that this row is not writable, so their shell builds no form and never asks where to write")
	}
	if operator.pointed == "" {
		t.Fatalf("a write at the tenant's own door answered %d and named no address, though the write stands at %s",
			operator.refusedStatus, operatorBoardDoor)
	}
	if operator.takenStatus != http.StatusOK {
		t.Errorf("the refusal sent that caller to %s and a write there answered %d, not the write taking it",
			operator.pointed, operator.takenStatus)
	}
	if operator.named != operator.pointed {
		t.Errorf("the catalogue printed write_path %q and the refusal sent the same caller to %q, which answered %d: one field, one mount, two addresses",
			operator.named, operator.pointed, operator.takenStatus)
	}
	if operator.named != operatorBoardDoor {
		t.Errorf("both documents named %q, want the operator's door %s this mount moved the write to", operator.named, operatorBoardDoor)
	}

	member := showDoors(t, false)
	if member.readStatus != http.StatusOK {
		t.Fatalf("a member read their own drawing row at %s and got %d: the read is theirs, so the entry below is about the write",
			tenantBoardDoor, member.readStatus)
	}
	if member.writable || member.named != "" {
		t.Errorf("the catalogue handed a member who holds no write an entry addressing %q as writable=%v; the write is the installation's, and a door they cannot write at is neither true nor theirs to be told",
			member.named, member.writable)
	}
	// The silence is only true if the door it declines to name refuses this caller.
	if member.refusedStatus == http.StatusOK {
		t.Errorf("a member with no write permission wrote through the tenant's own door, so the catalogue's silence above was about nothing")
	}
	if member.pointed != "" && member.takenStatus == http.StatusOK {
		t.Errorf("the refusal at the tenant's door sent a member who holds no write to %s and that answered %d: it takes their write, which the catalogue refused to name",
			member.pointed, member.takenStatus)
	}
}
