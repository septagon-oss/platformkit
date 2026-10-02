package rest_test

// split_write_pointer_test.go asks one question of the answer a withheld verb
// produces on a resource whose writes belong to the operator: when the kernel
// points a caller at the address that performs the write, does something take
// the write there?
//
// `Spec.Mount` composes `httpx.Resource.WritePath` from wherever the write router
// is mounted whenever the two prefixes differ (kit/rest/rest.go), and
// `API.methodNotAllowed` quotes that address at a caller who posted to the read
// door (kit/httpx/httpx.go, CodeWriteElsewhere). The composition of the pointer is
// not narrowed by `Operations`, so a resource that offers no write at all still
// names a write door — one that was never built anywhere.
//
// The fixture is the operator's own tenant reached at the installation's host, the
// one that `operator_read_test.go` composes, because that is the only place the
// control-plane address answers rather than the host gate's 404. The case that
// offers `Create` is the arm that shows the pointer arriving; the case that offers
// none is the one under test.

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// writePointer is the address a WRITE_ELSEWHERE refusal sends the caller to.
var writePointer = regexp.MustCompile(`write it at ([^"\\]+)`)

type splitWriteAnswers struct {
	// readDoorStatus/refusal is the answer at the collection address, which is
	// mounted for the reads.
	readDoorStatus int
	refusal        string
	// named, namedStatus is the pointer and the answer taken there with the same
	// verb it was promised at.
	named       string
	namedStatus int
	namedBody   string
	// collectionStatus is the reachability probe: the read that is offered works.
	collectionStatus int
}

// askTheSplitWriteDoor mounts an operator-written Spec, posts to its read door,
// and then posts to the address the refusal names.
func askTheSplitWriteDoor(t *testing.T, verbs []httpx.CRUD) splitWriteAnswers {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	f := &operatorReader{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "ops", Operator: true}, allow: true}
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Tenants: f, Conn: app, Authorize: f, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s := spec
	s.Operations = verbs
	s.OperatorWrite = true
	s.Mount(api.Surfaces(s.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}

	var out splitWriteAnswers
	seed(t, admin, "a row behind an operator's write door")
	// The reachability probe, at the door this Spec does mount. A caller with the
	// operator's grant reads the collection, so the answer below is about a verb
	// and not about a caller, a tenant or a resource nothing serves.
	out.collectionStatus, _ = call(t, router, http.MethodGet, "/api/v1/tasks/task", "")

	const body = `{"title":"written where nothing listens"}`
	out.readDoorStatus, out.refusal = call(t, router, http.MethodPost, "/api/v1/tasks/task", body)
	match := writePointer.FindStringSubmatch(out.refusal)
	if match != nil {
		out.named = match[1]
		out.namedStatus, out.namedBody = call(t, router, http.MethodPost, out.named, body)
	}
	return out
}

// TestAPointerToASplitWriteDoorNamesAnAddressThatTakesTheWrite holds the refusal to
// its own promise: `write it at X` has to name an address that takes the write.
// A resource whose writes are the operator's and whose operation set names no
// write mounts no write anywhere, so the honest answer at its read door is the
// router's own — nothing is served for this verb — and not a direction.
func TestAPointerToASplitWriteDoorNamesAnAddressThatTakesTheWrite(t *testing.T) {
	// The arm that shows the pointer arriving, and the case's passing branch:
	// with create offered the write router really is mounted there, so the same
	// refusal is true and the address named takes the POST.
	offered := askTheSplitWriteDoor(t, []httpx.CRUD{rest.List, rest.Read, rest.Create})
	if offered.collectionStatus != http.StatusOK {
		t.Fatalf("GET the collection = %d, want 200: the operator's read door is mounted", offered.collectionStatus)
	}
	if offered.named == "" {
		t.Fatalf("the read door answered %d %s, want %s naming the operator's address: with create offered the pointer is the whole point of the split",
			offered.readDoorStatus, head(offered.refusal), httpx.CodeWriteElsewhere)
	}
	if offered.namedStatus == http.StatusNotFound || offered.namedStatus == http.StatusMethodNotAllowed {
		t.Fatalf("the pointer of a resource that DOES offer create leads to POST %s = %d %s, so this fixture cannot reach the control plane and cannot answer the question below",
			offered.named, offered.namedStatus, head(offered.namedBody))
	}

	// The case under review: the same Spec with no write verb at all. The pointer
	// is composed the same way, and nothing is mounted where it points.
	withheld := askTheSplitWriteDoor(t, []httpx.CRUD{rest.List, rest.Read})
	if withheld.collectionStatus != http.StatusOK {
		t.Fatalf("GET the collection = %d, want 200", withheld.collectionStatus)
	}
	if withheld.readDoorStatus == http.StatusMethodNotAllowed {
		return // the honest answer: this address takes no write, and no other is claimed.
	}
	if withheld.named == "" {
		t.Fatalf("the read door answered %d %s, want either %d or %s",
			withheld.readDoorStatus, head(withheld.refusal), http.StatusMethodNotAllowed, httpx.CodeWriteElsewhere)
	}
	if withheld.namedStatus == http.StatusNotFound || withheld.namedStatus == http.StatusMethodNotAllowed {
		t.Errorf("%s points the caller to POST %s, which answers %d %s: this Spec offers no create, update or delete, so no write door was ever built, here or on the operator's surface",
			httpx.CodeWriteElsewhere, withheld.named, withheld.namedStatus, head(withheld.namedBody))
	}
	if !strings.Contains(withheld.refusal, httpx.CodeWriteElsewhere) {
		t.Errorf("the read door answered %d %s, which is neither the router's answer nor a refusal naming a way out",
			withheld.readDoorStatus, head(withheld.refusal))
	}
}
