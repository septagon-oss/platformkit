package rest_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

type operatorReader struct {
	tenant tenancy.Tenant
	allow  bool
	asked  int
}

func (f *operatorReader) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return f.tenant, nil
}

func (f *operatorReader) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	f.asked++
	return f.allow, nil // true is the customer's wildcard, not an operator grant
}

// Exercise the real middleware, database-backed closures and generated UI.
// A tenant wildcard must not reveal even the existence or count of private rows.
func TestOperatorReadBoundary(t *testing.T) {
	for _, shape := range []string{"collection", "singleton"} {
		for _, tc := range []struct {
			name                            string
			private, operator, allow, reads bool
		}{
			{"customer wildcard", true, false, true, false},
			{"operator permission", true, true, true, true},
			{"operator without permission", true, true, false, false},
			{"ordinary resource", false, false, true, true},
		} {
			t.Run(shape+"/"+tc.name, func(t *testing.T) {
				admin, app := dbtest.Schema(t)
				if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
					t.Fatal(err)
				}
				f := &operatorReader{tenant: tenancy.Tenant{ID: uuid.New(), Operator: tc.operator}, allow: tc.allow}
				rowID := uuid.New()
				const title = "private record value"
				if _, err := admin.ExecContext(t.Context(),
					"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, f.tenant.ID, title); err != nil {
					t.Fatal(err)
				}
				api, router := httpx.New(httpx.Options{
					PublicHost: host, Tenants: f, Conn: app, Authorize: f,
					// The operator's tenant is the installation's, and the
					// installation is reached at host: both facts are what makes
					// the private routes below reachable at all.
					Installation: host,
					Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
						return tenancy.Principal{UserID: principal}, true, nil
					},
					Log: slog.New(slog.DiscardHandler),
				})
				if shape == "collection" {
					s := spec
					s.OperatorRead, s.OperatorWrite = tc.private, true
					s.Mount(api.Surfaces(s.Module))
				} else {
					s := singleton(true, false)
					s.OperatorRead, s.OperatorWrite = tc.private, true
					s.Mount(api.Surfaces(s.Module))
				}
				r := api.Resources()[0]
				if r.OperatorRead != tc.private || !r.OperatorWrite {
					t.Fatal("resource lost the route's operator declarations")
				}
				opts := screens.Options{Workspace: "/app"}
				shell := page.Shell{Tag: "admin", Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
					return g.Group(body)
				}}
				screens.Mount(api.Surfaces(r.Module).App, shell, opts, r)
				for _, grant := range []tenancy.Grant{
					{Permission: "task:read", Operator: tc.private},
					{Permission: "task:write", Operator: true},
				} {
					if !slices.Contains(api.Required(), grant) {
						t.Errorf("route declarations omit %+v", grant)
					}
				}
				httpx.Register(api.Surfaces("tasks").App, huma.Operation{OperationID: "probe", Method: http.MethodGet, Path: "/probe"},
					httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{ Body screens.Catalog }, error) {
						if r.Readable(ctx) != tc.reads {
							t.Errorf("Readable = %v, want %v", r.Readable(ctx), tc.reads)
						}
						check := func(op string, err error) bool {
							if tc.reads {
								if err != nil {
									t.Errorf("%s: %v", op, err)
								}
								return err == nil
							}
							if p, ok := errors.AsType[*problem.Problem](err); !ok || p.Status != http.StatusForbidden {
								t.Errorf("%s = %v, want forbidden", op, err)
							}
							return false
						}
						count, err := r.Count(ctx) // Singleton uses the List-based fallback.
						if check("Count", err) && count != 1 {
							t.Errorf("Count = %d, want 1", count)
						}
						rows, total, err := r.List(ctx, crud.Query{Limit: 10})
						if check("List", err) && (total != 1 || len(rows) != 1 || rows[0]["title"] != title) {
							t.Errorf("List = %v, %d", rows, total)
						}
						row, err := r.Get(ctx, rowID)
						if check("Get", err) && row["title"] != title {
							t.Errorf("Get = %v", row)
						}
						return &struct{ Body screens.Catalog }{Body: screens.Describe(ctx, api.Resources())}, nil
					})
				if err := api.ValidateDeclarations(); err != nil {
					t.Fatal(err)
				}
				code, body := call(t, router, http.MethodGet, "/api/v1/tasks/probe", "")
				if code != http.StatusOK || strings.Contains(body, r.Schema.Path) != tc.reads {
					t.Errorf("discovery = %d %s, want visible=%v", code, body, tc.reads)
				}
				paths := []string{r.Schema.Path, r.Screen}
				gone := []string{}
				if shape == "collection" {
					paths = append(paths, r.Schema.Path+"/"+rowID.String(), r.Screen+"/"+rowID.String())
				} else {
					// A singleton's API has no id in its path, and neither may its screens.
					// This assertion used to demand 200 from the screen's id-shaped path, which
					// is how the defect survived: the route existed, so the check passed, and the
					// settings page it served listed one row, offered New, and linked an id of all
					// zeros. Reproduced and required absent in
					// TestTheScreensOfASingletonOfferOnlyWhatItsRoutesServe.
					gone = []string{r.Schema.Path + "/" + rowID.String(), r.Screen + "/" + rowID.String()}
				}
				for _, path := range paths {
					code, body := call(t, router, http.MethodGet, path, "")
					// Two doors, and now two refusals. The JSON route of a
					// resource the installation owns is mounted on the control
					// plane, and at a tenant that is not the installation's the
					// address answers the way an address nothing serves does —
					// 404, and the authorizer is never asked. The generated
					// screen is workspace work: it stays where a person stands
					// and refuses the caller it always refused, with 403.
					want := http.StatusForbidden
					switch {
					case tc.reads:
						want = http.StatusOK
					case tc.private && !tc.operator && strings.HasPrefix(path, r.Schema.Path):
						want = http.StatusNotFound
					}
					if code != want || strings.Contains(body, title) != tc.reads {
						t.Errorf("GET %s = %d %s, want %d with visible=%v", path, code, body, want, tc.reads)
					}
				}
				for _, path := range gone {
					// No route at all, whatever the caller's grants: a 403 here would mean a
					// route exists and is merely guarded, which is the shape of the bug.
					if code, _ := call(t, router, http.MethodGet, path, ""); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
						t.Errorf("GET %s = %d, want no such route behind a singleton's id", path, code)
					}
				}
				if tc.private && !tc.operator && f.asked != 0 {
					t.Errorf("customer wildcard was consulted %d times for operator reads", f.asked)
				}
				method, success := http.MethodPost, http.StatusCreated
				if shape == "singleton" {
					method, success = http.MethodPut, http.StatusOK
				}
				// The same split at the write door: an operator write refused
				// because the tenant is not the installation's is a 404 at the
				// control-plane address, and a 403 only where the caller's own
				// grants are the question.
				want := http.StatusForbidden
				if !tc.operator {
					want = http.StatusNotFound
				}
				if tc.operator && tc.allow {
					want = success
				}
				at := api.Surfaces(r.Module).Ops.Path(r.Path) // the write door of a resource the installation owns
				if code, body := call(t, router, method, at, `{"title":"updated by operator"}`); code != want {
					t.Errorf("operator write = %d %s, want %d", code, body, want)
				}
			})
		}
	}
}
