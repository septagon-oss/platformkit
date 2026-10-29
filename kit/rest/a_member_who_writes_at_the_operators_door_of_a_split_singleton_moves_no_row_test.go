package rest_test

// a_member_who_writes_at_the_operators_door_of_a_split_singleton_moves_no_row_test.go
// asks the operator's door directly, rather than following an address a refusal
// happens to name.
//
// A singleton whose write stands on the operator's surface refuses a tenant's PUT
// at their own door, and what a tenant caller is then told depends on what they
// hold: a caller who holds the write is pointed at the door that takes it, and a
// caller who holds none is pointed nowhere, because a door they cannot write at is
// neither true nor theirs to be told. A case that only ever asks the second kind of
// caller where the refusal pointed therefore reaches the operator's door with a
// tenant's session by accident or not at all — the assertion behind that door holds
// or not without changing what the case observes.
//
// So this case addresses the operator's door itself. A member of a tenant who is not
// the installation is refused there and the mount's save closure runs no time, and
// the installation who holds that write is taken, once. Both halves are read through
// a status code and the mount's own tally, so the case fails on the door's behaviour
// and not on the wording around it.

import (
	"context"
	"log/slog"
	"net/http"
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

// directDoorBoard is a tenant's one drawing row, mounted through closures so that the
// tally below is the whole of what the mount wrote.
type directDoorBoard struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (directDoorBoard) TableName() string { return "rest_direct_door_boards" }

// writeAtTheOperatorsDoor mounts one split singleton and PUTs to the operator's
// address with one caller — the installation's own tenant when `operator` is set, a
// plain member of the tenant otherwise. The caller holds the write permission in
// both cases: what parts the two is the surface, not the grant. The save closure
// counts itself, and the tally and the status come back.
func writeAtTheOperatorsDoor(t *testing.T, operator bool) (int, int) {
	t.Helper()
	_, app := dbtest.Schema(t)
	who := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	if operator {
		who = tenancy.Tenant{ID: uuid.New(), Slug: "ops", Operator: true}
	}
	ask := &operatorReader{tenant: who, allow: true}
	saves := 0
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: ask, Conn: app,
		Authorize: ask, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	board := rest.Singleton[*directDoorBoard]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write", OperatorWrite: true,
		Load: func(context.Context, db.Tx[db.Tenant]) (*directDoorBoard, error) {
			return &directDoorBoard{Drawing: entity.Sketch{{{0, 0, 0}}}}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *directDoorBoard) (*directDoorBoard, error) {
			saves++
			return in, nil
		},
	}
	board.Mount(api.Surfaces(board.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted doors do not declare themselves: %v", err)
	}
	code, _ := call(t, router, http.MethodPut, "/api/v1/ops/drawing/board",
		`{"drawing":[[[0,0,0],[1000,1000,12]]]}`)
	return code, saves
}

// TestAMemberWhoWritesAtTheOperatorsDoorOfASplitSingletonMovesNoRow holds the
// operator's write door of a split singleton to its declaration: it takes the write
// of the installation that owns it exactly once, and refuses a tenant member who
// holds the write permission but not the surface, having run the mount's save
// closure no time.
func TestAMemberWhoWritesAtTheOperatorsDoorOfASplitSingletonMovesNoRow(t *testing.T) {
	member, memberSaves := writeAtTheOperatorsDoor(t, false)
	if member == http.StatusOK {
		t.Errorf("a member of a tenant who is not the installation wrote the operator's door of a split singleton at /api/v1/ops/drawing/board and got %d, though the mount moved its write there: the surface is the installation's", member)
	}
	if memberSaves != 0 {
		t.Errorf("the write refused at /api/v1/ops/drawing/board ran the mount's save closure %d times: the caller was refused after the row moved, so their retry lands on what they were told was refused", memberSaves)
	}

	installation, installationSaves := writeAtTheOperatorsDoor(t, true)
	if installation != http.StatusOK {
		t.Errorf("the installation wrote at /api/v1/ops/drawing/board, where this mount puts its write, and got %d: the door the tenant is refused at must be the door that takes this write", installation)
	}
	if installationSaves != 1 {
		t.Errorf("the installation's one accepted write at /api/v1/ops/drawing/board ran the mount's save closure %d times: the door is one write, no more", installationSaves)
	}
}
