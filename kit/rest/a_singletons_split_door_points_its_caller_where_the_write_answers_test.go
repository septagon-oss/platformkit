package rest_test

// a_singletons_split_door_points_its_caller_where_the_write_answers_test.go asks
// the answer a mispointed write gives, of one row whose two doors were moved
// apart.
//
// Two clients read `httpx.Resource.WritePath`, and neither of them reads the
// served /openapi.json: the catalogue a phone reads prints it as `write_path`
// (ui/screens/catalog.go), and `API.methodNotAllowed` quotes it at a caller who
// wrote at the read door (kit/httpx/httpx.go, CodeWriteElsewhere). Both quote the
// one field, so both are wrong or both are right together — and a caller sent to
// an address that takes no write has no way out of the refusal, only a second one.
//
// rest.Singleton composes that field from wherever its doors landed once the
// operator grants are read, so the four grant pairs are four different answers.
// This case asks each pair the same thing at both doors: which answer takes the
// PUT, which takes the GET, and what the refusal at the other door tells the
// caller to do. A pointer is right only where it names the door that answered the
// PUT, which is the consumer's half of the sentence the mount writes.
//
// The fixture is the installation reached at the customer's host: the only place a
// control-plane door answers at all rather than the host gate answering 404.

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

// pointedDoorDrawing is one stroke of two points, well inside the bounds the value
// enforces, so the answer below is about an address and never about a body.
const pointedDoorDrawing = `[[[0,0,0],[1000,1000,12]]]`

// pointedDoorPointer is the way out a refusal offers: the address that takes the
// write this address does not.
var pointedDoorPointer = regexp.MustCompile(`write it at (/[^"\\\s]+)`)

// pointedDoorBoard is a tenant's one drawing row, the shape a module keeps for a
// drawing the tenant configures once and somebody else may hold the door to.
type pointedDoorBoard struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (pointedDoorBoard) TableName() string { return "rest_pointed_door_boards" }

// pointedDoorAnswer is what one mount said at each of its two doors. The doors are
// asked, not derived from the mount's fields: the question is whether what a caller
// is told matches where the verbs answer.
type pointedDoorAnswer struct {
	verbAt        map[string]string // verb -> the address that answered it, when one did
	bothAt        map[string]string // verb -> the other address, when that answered too
	statuses      map[string]int    // "GET /api/v1/…" and "PUT /api/v1/…" -> the status
	pointer       string            // the address named by a refusal that gave one
	pointerAt     string            // the door whose refusal named it
	pointerStatus int               // the answer taken at the address that was named
}

// askBothDoors mounts a tenant's one drawing row with the given operator grants and
// asks GET and PUT at the tenant's own door and at the operator's. A verb answered
// at both doors is recorded as bothAt rather than chosen between: one row has one
// address per verb, and a mount that served two would make the rest of this case
// meaningless.
func askBothDoors(t *testing.T, operatorRead, operatorWrite bool) pointedDoorAnswer {
	t.Helper()
	_, app := dbtest.Schema(t) // No table: the row is a closure's, the transaction is the tenant's.
	ask := &operatorReader{
		tenant: tenancy.Tenant{ID: uuid.New(), Slug: "ops", Operator: true},
		allow:  true,
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: host, Tenants: ask, Conn: app,
		Authorize: ask, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	board := rest.Singleton[*pointedDoorBoard]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write",
		OperatorRead: operatorRead, OperatorWrite: operatorWrite,
		Load: func(context.Context, db.Tx[db.Tenant]) (*pointedDoorBoard, error) {
			return &pointedDoorBoard{Drawing: entity.Sketch{{{0, 0, 0}}}}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *pointedDoorBoard) (*pointedDoorBoard, error) {
			return in, nil
		},
	}
	board.Mount(api.Surfaces(board.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted doors do not declare themselves: %v", err)
	}
	out := pointedDoorAnswer{
		verbAt:   map[string]string{},
		bothAt:   map[string]string{},
		statuses: map[string]int{},
	}
	const body = `{"drawing":` + pointedDoorDrawing + `}`
	for _, verb := range []string{http.MethodGet, http.MethodPut} {
		for _, at := range []string{"/api/v1/drawing/board", "/api/v1/ops/drawing/board"} {
			code, _ := call(t, router, verb, at, body)
			out.statuses[verb+" "+at] = code
			if code < http.StatusOK || code >= http.StatusMultipleChoices {
				continue
			}
			if seen, held := out.verbAt[verb]; held {
				out.bothAt[verb] = seen
				continue
			}
			out.verbAt[verb] = at
		}
	}
	// The way out the kernel offered at whichever door refused the write.
	for _, at := range []string{"/api/v1/drawing/board", "/api/v1/ops/drawing/board"} {
		if out.verbAt[http.MethodPut] == at {
			continue
		}
		_, answer := call(t, router, http.MethodPut, at, body)
		if match := pointedDoorPointer.FindStringSubmatch(answer); match != nil {
			out.pointer = match[1]
			out.pointerAt = at
			out.pointerStatus, _ = call(t, router, http.MethodPut, out.pointer, body)
		}
	}
	return out
}

// TestASingletonsSplitDoorPointsItsCallerWhereTheWriteAnswers holds the answer a
// refused write gives to the door that actually takes it. One tenant row mounted
// four ways answers at one address per verb; where the read and the write stand at
// two, a caller who writes at the read door is refused, and the refusal has to name
// the address that takes the write — the same address the catalogue of that caller
// prints as write_path. Where the two doors share an address the write answers
// there outright, and no refusal is asked.
func TestASingletonsSplitDoorPointsItsCallerWhereTheWriteAnswers(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		operatorRead, operatorWrite bool
	}{
		{"both doors are the tenant's", false, false},
		{"the read is the operator's", true, false},
		{"the write is the operator's", false, true},
		{"both doors are the operator's", true, true},
	} {
		got := askBothDoors(t, tc.operatorRead, tc.operatorWrite)
		readAt, reads := got.verbAt[http.MethodGet]
		writeAt, writes := got.verbAt[http.MethodPut]
		if other, both := got.bothAt[http.MethodGet]; both {
			t.Errorf("%s: GET answers at %s and at %s, so this mount has no one read address to be told", tc.name, readAt, other)
			continue
		}
		if other, both := got.bothAt[http.MethodPut]; both {
			t.Errorf("%s: PUT answers at %s and at %s, so this mount has no one write address to point at", tc.name, writeAt, other)
			continue
		}
		if !reads || !writes {
			t.Errorf("%s: the doors answer GET=%q PUT=%q (%v), so there is no address to hold the refusal to",
				tc.name, readAt, writeAt, got.statuses)
			continue
		}
		if readAt == writeAt {
			if got.pointer != "" {
				t.Errorf("%s: the write answers at %s and yet a refusal pointed a caller elsewhere, at %s from %s",
					tc.name, writeAt, got.pointer, got.pointerAt)
			}
			continue
		}
		// The doors part, so the write door has to be reachable from the read door's
		// own refusal: a caller holding the read address has nothing else.
		if got.pointer == "" {
			t.Errorf("%s: a PUT at %s is refused while the write stands at %s, and the refusal named no address to write at instead (%s=%d)",
				tc.name, readAt, writeAt, http.MethodPut+" "+readAt, got.statuses[http.MethodPut+" "+readAt])
			continue
		}
		if got.pointer != writeAt {
			t.Errorf("%s: the refusal at %s named %s, where the write does not stand: the PUT there answered %d while the PUT at %s answered %d",
				tc.name, got.pointerAt, got.pointer, got.pointerStatus, writeAt, got.statuses[http.MethodPut+" "+writeAt])
		}
		if got.pointerStatus != http.StatusOK {
			t.Errorf("%s: the refusal at %s sent the caller to %s and that answered %d, not the write taking it",
				tc.name, got.pointerAt, got.pointer, got.pointerStatus)
		}
	}
}
