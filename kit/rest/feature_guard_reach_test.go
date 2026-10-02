package rest_test

// feature_guard_reach_test.go pins how far a plan feature named in a declared
// guard reaches.
//
// `Spec.ReadAuth` may be `httpx.Permission("task:read").Needing("projects")`:
// that is the one spelling check() accepts from a declaration, because it is the
// only one that can say "the grant *and* the plan" (kit/rest operationsFault,
// R6). The route asks both questions — the Authorizer for the grant, then
// `API.entitled` for the plan, which answers 402 PLAN_EXCLUDES. The two doors
// that consult the *declaration* instead of the route, `Resource.Readable`/
// `Writable` (through `mayUse`) and the five closures (through `API.mayDeclare`),
// ask only the first. So the catalogue, the navigation, the dashboard card and
// every generated page's doors describe a resource whose every address the same
// caller is refused at — the exact shape this package refuses a New button for.

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// planWithout answers "not in the plan" to one named feature and "yes" to every
// other question, so a case can move the plan and nothing else.
type planWithout struct{ feature string }

func (p planWithout) Includes(_ context.Context, _ tenancy.Tenant, feature string) (bool, error) {
	return feature != p.feature, nil
}

// planEvery answers "in the plan" to everything: the arm that shows the case
// below is about the plan and not about a fixture that refuses by construction.
type planEvery struct{}

func (planEvery) Includes(context.Context, tenancy.Tenant, string) (bool, error) { return true, nil }

// mountEntitled is mountAs with the one collaborator it lacks: an Entitler, which
// a Spec whose declared guard names a feature has to be composed with —
// ValidateDeclarations refuses the composition without one.
func mountEntitled(t *testing.T, s rest.Spec[*Task], plan httpx.Entitler) (*httpx.API, chi.Router, *sql.DB) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatalf("create tasks: %v", err)
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: httpx.TenantLoader(caller{}), Conn: app, Authorize: caller{},
		Entitle: plan, Installation: host,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s.Mount(api.Surfaces(s.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return api, router, admin
}

// featureGuardSpec is the Spec this file is about: its read guard is a declared
// permission that also names a plan feature, which is why the shorthand cannot
// spell it (R6 refuses `Read: "task:read"` beside a declared ReadAuth, and the
// shorthand has no way to name a feature at all).
func featureGuardSpec() rest.Spec[*Task] {
	s := spec
	s.Read = ""
	s.ReadAuth = httpx.Permission("task:read").Needing("projects")
	return s
}

// askedBehindAFeature mounts a Spec, asks the *route*, then asks the same
// tenant's question of the closures and the catalogue from inside one request.
type askedBehindAFeature struct {
	routeStatus  int
	routeBody    string
	probeStatus  int
	probeBody    string
	closureErr   error
	closureRows  int64
	readable     bool
	writable     bool
	catalogued   int
	catalogWrite []bool
}

func askBehindAFeature(t *testing.T, s rest.Spec[*Task], plan httpx.Entitler) askedBehindAFeature {
	t.Helper()
	api, router, admin := mountEntitled(t, s, plan)
	seed(t, admin, "a row behind the plan feature")
	res := api.Resources()[0]
	var out askedBehindAFeature
	out.routeStatus, out.routeBody = call(t, router, http.MethodGet, "/api/v1/tasks/task", "")

	httpx.Register(api.Surfaces(s.Module).App, probeOperation("feature-guard-probe", "/feature-guard-probe"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			out.readable = res.Readable(ctx)
			out.writable = res.Writable(ctx)
			_, n, err := res.List(ctx, crud.Query{Limit: 10})
			out.closureErr, out.closureRows = err, n
			out.closureRows = countRows(t, ctx, res)
			catalog := screens.Describe(ctx, api.Resources())
			out.catalogued = len(catalog.Resources)
			for _, e := range catalog.Resources {
				out.catalogWrite = append(out.catalogWrite, e.Writable)
			}
			return nil, nil
		})
	out.probeStatus, out.probeBody = call(t, router, http.MethodPost,
		api.Surfaces(s.Module).App.Path("/feature-guard-probe"), "")
	return out
}

func countRows(t *testing.T, ctx context.Context, res httpx.Resource) int64 {
	t.Helper()
	rows, total, err := res.List(ctx, crud.Query{Limit: 10})
	if err != nil {
		return -1
	}
	_ = rows
	return total
}

// TestAClosureBehindAGuardThatNamesAPlanFeatureRefusesWhatItsRouteRefuses holds
// the closures to the same declaration as the routes. `API.mayDeclare` asks the
// Authorizer and the membership question and not the plan question, and its own
// comment says the feature "is asked by the middleware of the route the closure
// runs beneath, which is the same declaration". That holds for a generated page
// and for the API route; it does not hold for a caller that reaches the closure
// beneath some other route — the dashboard card calls `Resource.Count` from the
// dashboard's own page, and a module's own page calls `List` from its own guard —
// so the closure has to carry the question itself.
func TestAClosureBehindAGuardThatNamesAPlanFeatureRefusesWhatItsRouteRefuses(t *testing.T) {
	behind := askBehindAFeature(t, featureGuardSpec(), planWithout{"projects"})
	if behind.probeStatus != http.StatusNoContent {
		t.Fatalf("the probe route = %d %s, want %d: this case asks about the closures, and never reached them",
			behind.probeStatus, head(behind.probeBody), http.StatusNoContent)
	}
	// The reachability probe is the route's own answer, which is correct here and
	// correct after the fix: a plan that lacks the feature is refused at the door.
	if behind.routeStatus != http.StatusPaymentRequired {
		t.Fatalf("GET the collection = %d %s, want %d: the middleware asks the plan this Spec declared",
			behind.routeStatus, head(behind.routeBody), http.StatusPaymentRequired)
	}
	if behind.closureErr == nil {
		t.Errorf("Resource.List answered %d rows for a tenant whose plan lacks %q, though the route it is mounted behind refused the same read with %d: the closure asks the grant and not the plan",
			behind.closureRows, "projects", http.StatusPaymentRequired)
	} else if !refused(behind.closureErr, http.StatusPaymentRequired) {
		t.Errorf("Resource.List = %v, want the same refusal its route answers", behind.closureErr)
	}

	// And not the other way round: with the feature in the plan, the same closure
	// on the same Spec answers its rows, so the refusal above is about the plan.
	granted := askBehindAFeature(t, featureGuardSpec(), planEvery{})
	if granted.routeStatus != http.StatusOK {
		t.Fatalf("GET the collection with the feature in the plan = %d %s, want 200",
			granted.routeStatus, head(granted.routeBody))
	}
	if granted.closureErr != nil || granted.closureRows != 1 {
		t.Errorf("with the feature in the plan the closure = %v/%d rows, want no error and the one seeded row",
			granted.closureErr, granted.closureRows)
	}
}

// TestTheCatalogueOfATenantWhosePlanLacksTheGuardFeatureNamesNoResource holds the
// document to the same question. screens.Describe keeps a resource whose
// Readable answers no — "an unreadable resource is not in the document at all:
// what a caller may not look at, they are not told exists" (ui/screens Entry) —
// and Readable is `mayUse(ReadAuth)`, which answers yes to a caller whose plan
// refuses every verb the entry then lists. A phone given this entry offers the
// verbs, and every call answers 402.
func TestTheCatalogueOfATenantWhosePlanLacksTheGuardFeatureNamesNoResource(t *testing.T) {
	behind := askBehindAFeature(t, featureGuardSpec(), planWithout{"projects"})
	if behind.probeStatus != http.StatusNoContent {
		t.Fatalf("the probe route = %d %s, want %d", behind.probeStatus, head(behind.probeBody), http.StatusNoContent)
	}
	if behind.routeStatus != http.StatusPaymentRequired {
		t.Fatalf("GET the collection = %d %s, want %d", behind.routeStatus, head(behind.routeBody), http.StatusPaymentRequired)
	}
	if behind.readable {
		t.Errorf("Resource.Readable answered yes for a tenant every read of this resource refuses with %d: the entry it puts in the catalogue promises a verb nothing answers",
			http.StatusPaymentRequired)
	}
	if behind.catalogued != 0 {
		t.Errorf("the catalogue names %d resource(s) — writable %v — for a caller whose plan excludes the feature its guard names, want none",
			behind.catalogued, behind.catalogWrite)
	}

	// The same document with the feature in the plan does name it, writable as
	// the guard says: the case refuses the missing plan question, not the entry.
	granted := askBehindAFeature(t, featureGuardSpec(), planEvery{})
	if granted.catalogued != 1 {
		t.Fatalf("the catalogue names %d resource(s) with the feature in the plan, want the one the caller may read", granted.catalogued)
	}
	if !granted.readable {
		t.Error("Readable answered no for a caller the guard admits")
	}
}
