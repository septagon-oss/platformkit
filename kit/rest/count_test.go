package rest_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type countAuthorizer struct {
	allow  bool
	err    error
	checks int
}

func (a *countAuthorizer) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	a.checks++
	return a.allow, a.err
}

func TestResourceCountChecksPermissionOnceAndLoadsNoRows(t *testing.T) {
	for _, tt := range []struct {
		name   string
		allow  bool
		err    error
		status int
	}{
		{"allowed", true, nil, http.StatusOK},
		{"denied", false, nil, http.StatusForbidden},
		{"unavailable", false, errors.New("authorization offline"), http.StatusServiceUnavailable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			auth := &countAuthorizer{allow: tt.allow, err: tt.err}
			api, router, admin := mountAs(t, spec, auth)
			for i, tenant := range []uuid.UUID{acme.ID, acme.ID, uuid.New()} {
				if _, err := admin.ExecContext(t.Context(), "INSERT INTO rest_tasks (id, tenant_id, title, deleted_at) VALUES ($1, $2, $3, CASE WHEN $4 THEN now() END)", uuid.New(), tenant, uuid.NewString(), i == 1); err != nil {
					t.Fatal(err)
				}
			}
			r := api.Resources()[0]
			var queries []string
			httpx.Register(api.Surfaces("tasks").App, huma.Operation{OperationID: "count-probe", Method: http.MethodGet, Path: "/count", Hidden: true},
				httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{ Body int64 }, error) {
					tx, _ := httpx.TxFrom(ctx)
					query := tx.DB().Callback().Query()
					if err := query.After("gorm:query").Register("count-probe", func(g *gorm.DB) { queries = append(queries, g.Statement.SQL.String()) }); err != nil {
						t.Fatal(err)
					}
					defer query.Remove("count-probe")
					total, err := r.Count(ctx)
					if err != nil {
						return nil, err
					}
					return &struct{ Body int64 }{Body: total}, nil
				})
			code, body := call(t, router, http.MethodGet, "/api/v1/tasks/count", "")
			if code != tt.status || auth.checks != 1 {
				t.Fatalf("count = %d %s; authorization checks=%d", code, body, auth.checks)
			}
			if tt.allow {
				if strings.TrimSpace(body) != "1" || len(queries) != 1 || !strings.Contains(strings.ToLower(queries[0]), "count(*)") {
					t.Errorf("count=%s queries=%v; want one live tenant row, one COUNT", body, queries)
				}
			} else if len(queries) != 0 {
				t.Errorf("refused count queried rows: %v", queries)
			}
		})
	}
}
