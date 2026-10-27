package rest_test

// the_page_door_of_a_split_singleton_refuses_a_caller_without_the_operators_surface_test.go
// asks the mount's write *closure* — the door a drawn page calls — as a tenant's
// member who holds the write permission but not the operator's surface.
//
// A split singleton states its write guard twice: `API.admitGrant` refuses it on the
// way into a route, and `API.may` refuses it inside the closure a resource carries.
// The two are separate calls over the same declaration, and a page that holds a
// Resource calls the closure under whatever guard its own route carries — so the
// surface check has to stand in the closure, and nothing a route answers can show
// that it does.
//
// The route refusals are covered: a member at the operator's address is turned away
// there, and the catalogue names no writable address to a caller who may not write.
// The closure is covered for the *permission* half (a caller who lacks the
// permission is refused and the row stands) and for the operator's *read* half, and
// no case calls this closure as a caller who holds the permission in a tenant that
// is not the installation's. What that leaves unheld is the half this case holds: the
// page's write of the installation's drawing must be refused with the mount's save
// closure run no time, rather than taken because the roles table says yes.
//
// Both halves are read as a status, a code and the mount's own tally, so the case
// fails on the door's behaviour and not on the wording around it.

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
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
)

// pageDoorBoard is a tenant's one drawing row with the installation holding the
// door to it. It declares its own table and never touches it: the row is its
// closures', and what matters here is whether a closure ran at all.
type pageDoorBoard struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (pageDoorBoard) TableName() string { return "rest_page_door_boards" }

// drawnByThePage is the drawing a page sends: a stroke the mount's Load does not
// answer, so a save closure that ran is told from one that did not.
var drawnByThePage = entity.Sketch{{{0, 0, 0}, {420, 300, 12}}}

// pageDoor is one caller's page meeting the mount's write closure: what the page
// request answered (which says the closure was reached), how the closure answered
// (0 where it was taken), the reason it refused, and how many times the mount's own
// save closure ran and with what drawing.
type pageDoor struct {
	probe             int
	status            int
	refused           bool
	reason            string
	saves             int
	drawnByTheSaveRun entity.Sketch
}

// meetThePageDoor mounts one split singleton and meets its write closure from
// inside a signed-in page request — the way a generated form meets it — as one
// caller: the installation's own tenant when `operator` is set, a plain member of
// the tenant otherwise. The caller holds the write permission in both cases; what
// parts the two is the surface, not the grant.
func meetThePageDoor(t *testing.T, operator bool) pageDoor {
	t.Helper()
	_, app := dbtest.Schema(t) // No table: the row is a closure's, the transaction is the request's.
	who := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	if operator {
		who = tenancy.Tenant{ID: uuid.New(), Slug: "ops", Operator: true}
	}
	ask := &operatorReader{tenant: who, allow: true} // allow is the customer's wildcard, not an operator grant
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: ask, Conn: app,
		Authorize: ask, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	var got pageDoor
	board := rest.Singleton[*pageDoorBoard]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write", OperatorWrite: true,
		Load: func(context.Context, db.Tx[db.Tenant]) (*pageDoorBoard, error) {
			return &pageDoorBoard{Drawing: entity.Sketch{{{0, 0, 0}}}}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *pageDoorBoard) (*pageDoorBoard, error) {
			got.saves++
			got.drawnByTheSaveRun = in.Drawing
			return in, nil
		},
	}
	board.Mount(api.Surfaces(board.Module))

	// The page itself: a hidden route under the ordinary signed-in guard, which is
	// how a module's own page reaches a Resource — beneath its own guard, not the
	// resource's. What the closure answered is recorded, not returned: a returned
	// error would travel the kernel's own mapping to a client, and this case is
	// about the answer the closure gave before that.
	var seen []string
	inPage(t, api, "/operators-door", "operators-door", &seen,
		func(ctx context.Context, r httpx.Resource, _ *[]string) {
			_, err := r.Update(ctx, uuid.New(), map[string]any{"drawing": drawnByThePage})
			got.refused = err != nil
			got.status = faultStatus(err)
			if err != nil {
				got.reason = err.Error()
			}
		})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted doors do not declare themselves: %v", err)
	}
	got.probe, _ = askAs(t, router, host, http.MethodPost,
		api.Surfaces(catalogModule).App.Path("/operators-door"), "")
	for _, f := range seen {
		t.Error(f)
	}
	return got
}

// TestThePageDoorOfASplitSingletonRefusesACallerWithoutTheOperatorsSurface holds the
// write closure of a mount whose write stands on the operator's surface to the same
// declaration its route holds: a member of a tenant who holds the write permission
// but is not the installation is refused inside the closure, having run the mount's
// save closure no time, and the installation who owns that write is taken there once,
// with the drawing the page sent.
func TestThePageDoorOfASplitSingletonRefusesACallerWithoutTheOperatorsSurface(t *testing.T) {
	member := meetThePageDoor(t, false)
	// The reachability probe is the page's own answer: 204 says the request reached
	// the handler and the closure ran to an answer, so a refusal below is about the
	// door and not about a probe nothing reached.
	if member.probe != http.StatusNoContent {
		t.Fatalf("the page that calls the mount's write closure answered %d: the closure was never reached, so nothing here can say what the door answered", member.probe)
	}
	if !member.refused {
		t.Errorf("the page's Update of a split singleton's drawing was taken from a member of a tenant who is not the installation (mount's save closure ran %d times): this mount put its write on the operator's surface, and the closure is that write", member.saves)
	} else if member.status != http.StatusForbidden {
		t.Errorf("the page's Update of a split singleton's drawing refused a tenant's member with %d: the refusal a closure owes is the one its route's declaration states — %q", member.status, member.reason)
	} else if !strings.Contains(member.reason, httpx.CodeNotOperator) {
		t.Errorf("the page's Update refused a tenant's member without naming the boundary they crossed: %q — a caller who holds the permission is told which other thing they do not hold", member.reason)
	}
	if member.saves != 0 {
		t.Errorf("the page's refused write ran the mount's save closure %d times, last holding %v: a refused mutation writes nothing, and a caller told they may not write who finds their drawing stored cannot retry", member.saves, member.drawnByTheSaveRun)
	}

	installation := meetThePageDoor(t, true)
	if installation.probe != http.StatusNoContent {
		t.Fatalf("the page that calls the mount's write closure answered %d for the installation: nothing below can say what the door did", installation.probe)
	}
	if installation.refused {
		t.Errorf("the installation was refused the write closure of a mount that puts its write on the operator's surface: %d %q", installation.status, installation.reason)
	}
	if installation.saves != 1 {
		t.Errorf("the installation's one accepted page write ran the mount's save closure %d times: the door is one write, no more", installation.saves)
	}
	want, err := drawnByThePage.MarshalJSON()
	if err != nil {
		t.Fatalf("the drawing this case sends does not encode: %v", err)
	}
	held, err := installation.drawnByTheSaveRun.MarshalJSON()
	if err != nil {
		t.Fatalf("the drawing the save closure holds does not encode: %v", err)
	}
	if string(held) != string(want) {
		t.Errorf("the page write the installation was taken left %s where the mount was handed %s: the closure took a write and stored something else", held, want)
	}
}
