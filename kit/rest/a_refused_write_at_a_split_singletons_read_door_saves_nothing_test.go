package rest_test

// a_refused_write_at_a_split_singletons_read_door_saves_nothing_test.go asks what a
// refused write costs the mount that refuses it.
//
// A singleton whose write stands on the operator's surface answers a tenant's PUT at
// their own door with a refusal, and every case in this package reads that answer:
// the address it names, the doors it is spoken at, the words around it. None asks the
// other half, which is the half the runtime owns — whether the mount's save closure
// ever ran. A 403 spoken after the write is a stored row with a refusal on top of it:
// the caller who reads it would retry, and the row they retry into already holds what
// they were told was refused.
//
// So the closure here counts its own calls, and the count is read in the order a
// caller meets the doors: after the read, after the refusal, after the door the
// refusal names. Zero after the read and after the refusal is the refusal being true;
// one more after the named door is that same refusal having been a way out and not a
// wall. For a caller who holds no write the count must stay zero at every door, the
// named one included.

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
)

const (
	quietDoorReadAt  = "/api/v1/drawing/board"
	quietDoorWriteAt = "/api/v1/ops/drawing/board"
)

// quietDoorPointer is the way out a refusal offers, and the only address the caller
// below acts on.
var quietDoorPointer = regexp.MustCompile(`write it at (/[^"\\\s]+)`)

// quietBoard is a tenant's one drawing row. It is mounted through closures and opens
// no table, which is what lets the count below be the whole of what the mount wrote.
type quietBoard struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (quietBoard) TableName() string { return "rest_quiet_door_boards" }

// quietWalk is one caller's visit to one mount: the three answers they got, the
// address the refusal named, and the mount's own tally after each of the three.
type quietWalk struct {
	readStatus, refusedStatus, namedStatus int
	named                                  string
	afterRead, afterRefusal, afterNamed    int
}

// writeQuietly mounts a tenant's one drawing row with its write moved to the
// operator's surface and visits it as one caller — the installation's own when
// `operator` is set, a member of the tenant otherwise: the read, then a write at the
// tenant's own door, then a write at the door the refusal names, if it named one. The
// save closure returns what it was handed and counts itself, so the tally is the
// mount's record of what reached it rather than what a response claimed.
func writeQuietly(t *testing.T, operator bool) quietWalk {
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
	board := rest.Singleton[*quietBoard]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write", OperatorWrite: true,
		Load: func(context.Context, db.Tx[db.Tenant]) (*quietBoard, error) {
			return &quietBoard{Drawing: entity.Sketch{{{0, 0, 0}}}}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *quietBoard) (*quietBoard, error) {
			saves++
			return in, nil
		},
	}
	board.Mount(api.Surfaces(board.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted doors do not declare themselves: %v", err)
	}
	const body = `{"drawing":[[[0,0,0],[1000,1000,12]]]}`

	var got quietWalk
	got.readStatus, _ = call(t, router, http.MethodGet, quietDoorReadAt, "")
	got.afterRead = saves
	var refusal string
	got.refusedStatus, refusal = call(t, router, http.MethodPut, quietDoorReadAt, body)
	got.afterRefusal = saves
	got.namedStatus = got.refusedStatus
	if match := quietDoorPointer.FindStringSubmatch(refusal); match != nil {
		got.named = match[1]
		got.namedStatus, _ = call(t, router, http.MethodPut, match[1], body)
		got.afterNamed = saves
	}
	return got
}

// TestARefusedWriteAtASplitSingletonsReadDoorSavesNothing holds a mount that refuses a
// write to having written nothing: its save closure runs no time for the read, no time
// for the refusal, and one more time only where the refusal's own address took the
// write — and stays at no time at every door for the caller who holds no write.
func TestARefusedWriteAtASplitSingletonsReadDoorSavesNothing(t *testing.T) {
	operator := writeQuietly(t, true)
	if operator.readStatus != http.StatusOK {
		t.Fatalf("the installation read the tenant's drawing row at %s and got %d: the row is served, so the writes below are about doors",
			quietDoorReadAt, operator.readStatus)
	}
	if operator.afterRead != 0 {
		t.Fatalf("a GET at %s ran the mount's save closure %d times, so nothing below can say what a refusal cost",
			quietDoorReadAt, operator.afterRead)
	}
	if operator.refusedStatus == http.StatusOK {
		t.Fatalf("a write at %s answered %d though the write stands at %s: there is no refusal here to hold to its promise",
			quietDoorReadAt, operator.refusedStatus, quietDoorWriteAt)
	}
	if operator.afterRefusal != 0 {
		t.Errorf("the write at %s answered %d and the mount had saved %d times by then: the refusal is spoken after the row moved",
			quietDoorReadAt, operator.refusedStatus, operator.afterRefusal)
	}
	if operator.named == "" {
		t.Fatalf("the refusal at %s answered %d and named no address, so the way out below was never offered",
			quietDoorReadAt, operator.refusedStatus)
	}
	if operator.named != quietDoorWriteAt {
		t.Errorf("the refusal named %q, want the operator's door %s this mount moved the write to",
			operator.named, quietDoorWriteAt)
	}
	if operator.namedStatus != http.StatusOK {
		t.Fatalf("the refusal sent that caller to %s and a write there answered %d, so the tally below counts no taken write",
			operator.named, operator.namedStatus)
	}
	if operator.afterNamed != operator.afterRefusal+1 {
		t.Errorf("the write the refusal pointed at ran the save closure %d times on top of the %d the refusal left: the caller was sent to one write and met %d",
			operator.afterNamed, operator.afterRefusal, operator.afterNamed-operator.afterRefusal)
	}

	member := writeQuietly(t, false)
	if member.readStatus != http.StatusOK {
		t.Fatalf("a member read their own drawing row at %s and got %d: the read is theirs, so the writes below are about doors",
			quietDoorReadAt, member.readStatus)
	}
	if member.afterRefusal != 0 {
		t.Errorf("a member who holds no write was refused at %s and the mount had saved %d times by then: the refusal kept nothing back",
			quietDoorReadAt, member.afterRefusal)
	}
	if member.namedStatus == http.StatusOK {
		t.Errorf("the door the refusal named, %s, took a write from a member who holds none", member.named)
	}
	if member.afterNamed != 0 {
		t.Errorf("every door refused this member and the mount saved %d times across them: a refused caller wrote to a row they may not write",
			member.afterNamed)
	}
}
