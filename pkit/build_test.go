package pkit_test

// Build is the phase a client's boot is judged by: everything about the
// composition has to be answered before the first effect, because the effect
// that follows is a migration and a migration is the point of no easy return
// (decision 0074 rule 1). These cases run against a real database created empty
// for the case, so "nothing happened" is a query and not a claim.

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	cartcontracts "github.com/septagon-oss/platformkit/pkit/internal/fixture/cart/contracts"
)

// doors answers the three questions every application must answer and no module
// can answer for another one: which host is which tenant, who is calling, and
// what they may do. It is the fixture's stand-in for the tenant and auth modules
// of a real application, and it is composed as any other module is — a
// declaration of each contract and a put of the value — which is what makes the
// refusals below the mechanism's rather than a special case's.
var doors = pkit.NewModule("doors", func(w *pkit.Wiring) (module.Module, error) {
	pkit.Put[httpx.TenantLoader](w, hosts{})
	pkit.Put[httpx.Authorizer](w, grants{})
	pkit.Put[pkit.Authenticator](w, pkit.Authenticator(caller))
	return module.Module{Name: "doors"}, nil
},
	pkit.Provides[httpx.TenantLoader](),
	pkit.Provides[httpx.Authorizer](),
	pkit.Provides[pkit.Authenticator](),
)

type hosts struct{}

func (hosts) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{}, tenancy.ErrNoSuchHost
}

type grants struct{}

func (grants) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return false, nil
}

func caller(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
	return tenancy.Principal{}, false, nil
}

// desk is the workspace. kit/app refuses a composition that mounts nothing on
// the workspace surface — a product without a place to stand is not a product —
// so the case that reaches the effects composes one screen of its own.
type deskIn struct{}

type deskOut struct {
	Note string `json:"note"`
}

var desk = pkit.NewModule("desk", func(*pkit.Wiring) (module.Module, error) {
	return module.Module{Name: "desk", Routes: func(r httpx.Surfaces) {
		httpx.Register(r.App, huma.Operation{Method: "GET", OperationID: "desk.note", Path: "/note"},
			httpx.SignedIn(), func(context.Context, *deskIn) (*deskOut, error) {
				return &deskOut{Note: "a desk with one note on it"}, nil
			})
	}}, nil
})

// wantsCart cannot be built until somebody answers its need, and the composition
// below composes nobody who does.
var wantsCart = pkit.NewModule("wishlist", func(*pkit.Wiring) (module.Module, error) {
	return module.Module{Name: "wishlist"}, nil
}, pkit.Needs[cartcontracts.Service]())

// onOneDatabase is the configuration Build is pointed at: a schema of the test's
// own, created empty, with the two roles the development database has.
func onOneDatabase(t *testing.T) config.Config {
	t.Helper()
	admin, application := dbtest.URLsFor(t)
	return config.Config{
		Server:   config.Server{Addr: "127.0.0.1:0", PublicHost: "collect.test"},
		Database: config.Database{URL: application, MigrateURL: admin},
		Log:      config.Log{Level: "error"},
	}
}

// tablesIn is every table the schema holds. It is the whole of what "no effect"
// means when a migration is what an effect would be: a migration that ran leaves
// its ledger behind, and a build that refused before it never can.
func tablesIn(t *testing.T, url string) int {
	t.Helper()
	pool, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer pool.Close()
	var n int
	err = pool.QueryRowContext(t.Context(),
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = current_schema()").Scan(&n)
	if err != nil {
		t.Fatalf("count tables: %v", err)
	}
	return n
}

func TestBuildRefusesAMissingProviderBeforeAnyEffect(t *testing.T) {
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(wantsCart, doors)

	_, err := a.Build(t.Context(), pkit.Deployment{Environment: pkit.Development, Config: cfg})
	says(t, err, "cartcontracts.Service")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind: the migration ran before the composition was answered", got)
	}
}

func TestBuildRefusesARequiredPortWithItsOwnSentence(t *testing.T) {
	// An app with no doors: nothing answers which host is which tenant. The
	// engine has its own sentence for a nil Options.Tenants; the case asserts
	// pkit's, because the engine's names a field and this one names the module
	// that has to answer the question.
	a := pkit.NewApp("collect").Use(cart.Module)
	_, err := a.Build(t.Context(), pkit.Deployment{Environment: pkit.Development,
		Config: onOneDatabase(t)})
	if err == nil {
		t.Fatal("an app with no tenant loader built")
	}
	if !strings.HasPrefix(err.Error(), "pkit: collect: Build:") {
		t.Errorf("the refusal is the engine's and not the composition's: %v", err)
	}
	says(t, err, "httpx.TenantLoader")
	says(t, err, "Put one in the module that knows which host is which tenant")
}

func TestBuildAnswersEveryMissingPortAtOnce(t *testing.T) {
	// One refusal per unanswered question, not one per boot: an operator who is
	// told about one door at a time fixes this build in three restarts.
	a := pkit.NewApp("collect").Use(cart.Module)
	_, err := a.Build(t.Context(), pkit.Deployment{Environment: pkit.Development, Config: onOneDatabase(t)})
	for _, want := range []string{"httpx.TenantLoader", "httpx.Authorizer", "pkit.Authenticator"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the joined refusal never names %s:\n%v", want, err)
		}
	}
}

func TestBuildRunsTheEffectsAndRefusesTheSecondBuild(t *testing.T) {
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(doors, desk)
	rt, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("a composition with every port answered was refused: %v", err)
	}
	defer rt.Close()
	if rt.Handler() == nil {
		t.Error("a built app has no handler")
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got == 0 {
		t.Error("a build that succeeded migrated nothing: the effects did not run")
	}
	if _, err := a.Build(t.Context(), buildDeployment(cfg, app.All)); err == nil ||
		!strings.Contains(err.Error(), "a new lifecycle needs a new App") {
		t.Errorf("a second Build on one App was not refused: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Errorf("Close twice: %v", err)
	}
}

func TestMustBuildPanicsWithBuildsSentence(t *testing.T) {
	a := pkit.NewApp("collect").Use(wantsCart, doors)
	_, want := a.Build(t.Context(), pkit.Deployment{Environment: pkit.Development})

	other := pkit.NewApp("collect").Use(wantsCart, doors)
	defer func() {
		got := recover()
		if got == nil {
			t.Fatal("MustBuild returned instead of panicking on a composition that does not resolve")
		}
		if text, isString := got.(string); !isString || text != want.Error() {
			t.Errorf("MustBuild panicked with %v; Build says %v", got, want)
		}
	}()
	other.MustBuild(t.Context(), pkit.Deployment{Environment: pkit.Development})
}

func TestRunRefusesBeforeItsFirstEffect(t *testing.T) {
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(wantsCart, doors)
	err := a.Run(t.Context(), buildDeployment(cfg, app.All), app.All)
	if err == nil || !strings.Contains(err.Error(), "cartcontracts.Service") {
		t.Fatalf("Run did not refuse the composition: %v", err)
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("Run left %d tables behind", got)
	}
}

// buildDeployment is the process the two lifecycle cases run: this schema, this
// role, and the in-process transport a combined role defaults to.
func buildDeployment(cfg config.Config, role app.Role) pkit.Deployment {
	return pkit.Deployment{Environment: pkit.Development, Config: cfg,
		Transports: app.Transports{Memory: memory.New}}
}

// controlPlane defines the two shapes of permission a role can name: one a
// tenant's own role may hold, and one that reaches the control plane. It is
// spelled here rather than added to pkit/internal/fixture because the manifest
// a role is read against is the whole subject of these cases, and widening a
// shared fixture would put a permission in front of every other case that
// composes it, for no assertion's sake.
var controlPlane = pkit.NewModule("control", func(*pkit.Wiring) (module.Module, error) {
	return module.Module{Name: "control", Permissions: []module.Permission{
		{Key: "reports:read", Label: "read the reports"},
		{Key: "tenant:manage", Label: "manage the tenant", Operator: true},
	}}, nil
})

func TestBuildRefusesAnUnansweredPortBeforeAnyEffect(t *testing.T) {
	// The two earlier table counts pin the resolver's phase; this one pins the
	// phase after it. Move the port check into kit/app's effect half — where the
	// engine's own `Options.Tenants is required` sentence already lives — and
	// this build migrates before it answers, which is the ordering decision 0074
	// rule 1 exists to refuse, and the count says so in the same words as every
	// other case here.
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(cart.Module)
	_, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
	says(t, err, "httpx.TenantLoader")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind: the ports were answered after the migration, not before it", got)
	}
}

func TestBuildRefusesARoleGrantingWhatNoComposedModuleDefines(t *testing.T) {
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(doors, desk).Roles(pkit.Role{
		Name: "coordinator", Grants: []string{"task:read"},
	})
	_, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
	says(t, err, "Roles")
	says(t, err, "coordinator grants task:read")
	says(t, err, "no composed module defines")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind", got)
	}
}

func TestBuildRefusesARoleGrantingAnOperatorPermission(t *testing.T) {
	// The grant exists — the module composed here defines it — so this is not a
	// composition the app can correct by composing something: the sentence says
	// what a tenant's role may not reach, and it is read off the operator flag
	// the defining module set, not off a name this package happens to know.
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(doors, desk, controlPlane).Roles(pkit.Role{
		Name: "admin", Grants: []string{"tenant:manage"},
	})
	_, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
	says(t, err, "admin grants tenant:manage")
	says(t, err, "reaches the control plane")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("the refused build left %d tables behind", got)
	}
}

func TestBuildAcceptsARoleItsComposedModulesDefine(t *testing.T) {
	// The pair the two refusals need: a grant that is answered is not refused,
	// and the phase that decides it still runs before the migration rather than
	// instead of it — the count here is the same query, answered above zero.
	cfg := onOneDatabase(t)
	a := pkit.NewApp("collect").Use(doors, desk, controlPlane).Roles(pkit.Role{
		Name: "reader", Grants: []string{"reports:read"},
	})
	rt, err := a.Build(t.Context(), buildDeployment(cfg, app.All))
	if err != nil {
		t.Fatalf("a role holding a permission its modules define was refused: %v", err)
	}
	defer rt.Close()
	if got := tablesIn(t, cfg.Database.MigrateURL); got == 0 {
		t.Error("a build that succeeded migrated nothing")
	}
}
