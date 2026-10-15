package internal_test

// At the trail. The tenant module writes one lifecycle verb
// as two events in two tenants — the verb in the subject's scope and `tenant.lifecycle_recorded`
// in the installation's — and the sentence the delivery writes about the second row is a
// tenancy claim: it is "what an operator's audit of the control plane reads", it names the
// subject's slug and id, and it "is still readable after the customer's own trail has been
// retained away or had its tenant deleted" (modules/tenant/internal/service.go, `record`).
//
// What that claim needs in order to be more than a comment is a trail whose rows are read by
// the tenant they belong to and by nobody else — including a tenant that is *not* the subject.
// `TestALifecycleCommandWritesTwoRowsWithOneTraceAndTwoScopes` pins the subject's side of that,
// in the tenant module, against the outbox. Nothing pinned the third tenant's side at the
// place the boundary is actually enforced: the trail table, read through the module's own List.
// A mirror row readable from a stranger's transaction would be the platform telling every
// customer which verbs its neighbours had used on them — the one fact the operator's trail
// exists to keep to itself.
//
// The case reaches every assertion through what the correct behaviour stores: a row count and
// a name read back through `Service.List` in each tenant's own transaction, and the trace the
// request carried. It reads no message and no error string, and it passes on any code that
// keeps the mirror where the module says it keeps it.

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// operatorTenant is the installation's own tenant — the scope a control-plane command mirrors
// its audit row into. It is written here rather than imported from the tenant module: the
// trail does not know what an operator is, only whose transaction wrote a row, which is the
// point the case is making.
var operatorTenant = tenancy.Tenant{
	ID: uuid.New(), Slug: "installation", Name: "This installation", Operator: true,
}

func TestTheOperatorsTrailOfAnotherTenantsActIsReadableOnlyByTheOperator(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()
	tr := trace.New()

	verbID, mirrorID := uuid.New(), uuid.New()
	at := db.Now()
	// The two rows one control-plane command writes, with the two event ids it writes them
	// with and the one traceparent it writes both with — the shape
	// modules/tenant/internal/service.go's `record` produces, reached here at the owner of
	// the table because no subscription runs in this test.
	subject := events.Event{
		ID: verbID, TenantID: acme.ID, Name: "tenant.tenant.suspended", At: at,
		TraceParent: tr.Parent(),
		Payload:     []byte(`{"tenantId":"` + acme.ID.String() + `","slug":"acme"}`),
	}
	mirror := events.Event{
		ID: mirrorID, TenantID: operatorTenant.ID, Name: "tenant.tenant.lifecycle_recorded", At: at,
		TraceParent: tr.Parent(),
		Payload:     []byte(`{"verb":"suspend","tenantId":"` + acme.ID.String() + `","slug":"acme"}`),
	}
	for _, ev := range []events.Event{subject, mirror} {
		tenant := acme
		if ev.TenantID == operatorTenant.ID {
			tenant = operatorTenant
		}
		if err := db.Run(tenancy.WithTenant(trace.With(t.Context(), tr), tenant), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error { return svc.Record(ctx, tx, ev) }); err != nil {
			t.Fatalf("record %s in %s's trail: %v", ev.Name, tenant.Slug, err)
		}
	}

	// Each trail, as the module reads it. What a tenant may read is its own rows; what the
	// operator may read is its own rows, which name another tenant by slug and by id.
	cases := []struct {
		who    tenancy.Tenant
		names  []string
		traces []string
	}{
		{who: acme, names: []string{subject.Name}, traces: []string{tr.Parent()}},
		{who: globex, names: nil, traces: nil},
		{who: operatorTenant, names: []string{mirror.Name}, traces: []string{tr.Parent()}},
	}
	for _, c := range cases {
		var (
			gotNames, gotTraces []string
			total               int64
		)
		err := db.Run(tenancy.WithTenant(t.Context(), c.who), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				rows, n, err := svc.List(ctx, tx, contracts.Query{Limit: 10})
				if err != nil {
					return err
				}
				total, gotNames, gotTraces = n, namesOf(rows), tracesOf(rows)
				return nil
			})
		if err != nil {
			t.Fatalf("read %s's trail: %v", c.who.Slug, err)
		}
		if !slices.Equal(gotNames, c.names) || total != int64(len(c.names)) {
			t.Errorf("%s's trail holds %d rows (%v), want %v. The two trails one lifecycle verb "+
				"writes are two tenants' rows: the subject's and the installation's, and a third "+
				"tenant that can read either has the operator's audit of its neighbours.",
				c.who.Slug, total, gotNames, c.names)
		}
		if !slices.Equal(gotTraces, c.traces) {
			t.Errorf("%s's trail carries traces %v, want %v", c.who.Slug, gotTraces, c.traces)
		}
	}

	// The operator's half is the operator's *and it says what it is about*: the mirror is
	// only an audit of the control plane if the operator can read which customer the verb
	// named. Read back through the contract, not through a raw column.
	var rows []*contracts.Event
	err := db.Run(tenancy.WithTenant(t.Context(), operatorTenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			rows, _, err = svc.List(ctx, tx, contracts.Query{Name: mirror.Name, Limit: 10})
			return err
		})
	if err != nil {
		t.Fatalf("read the operator's mirror row: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("the operator's trail holds %d mirror rows, want the one the verb wrote", len(rows))
	}
	var payload struct {
		Verb     string    `json:"verb"`
		TenantID uuid.UUID `json:"tenantId"`
		Slug     string    `json:"slug"`
	}
	if err := json.Unmarshal(rows[0].Payload, &payload); err != nil {
		t.Fatalf("the mirror's payload is not the shape the contract declares: %v", err)
	}
	if payload.Verb != "suspend" || payload.TenantID != acme.ID || payload.Slug != acme.Slug {
		t.Errorf("the operator's mirror reads %+v, want the verb, the subject and its slug", payload)
	}
	if rows[0].Traceparent != tr.Parent() {
		t.Errorf("the operator's mirror carries trace %q, want the request's %q", rows[0].Traceparent, tr.Parent())
	}
}

// namesOf is a page of the trail as the event names it holds, which is what these cases
// compare and the shortest way to say what a tenant was handed.
func namesOf(rows []*contracts.Event) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}
