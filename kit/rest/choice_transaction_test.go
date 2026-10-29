package rest_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type choiceRecord struct {
	crud.Base
	Shapes   []string `json:"shapes" gorm:"serializer:json"`
	Revision int64    `json:"revision" gorm:"not null;default:1" readOnly:"true"`
}

func (*choiceRecord) TableName() string { return "rest_choice_records" }

type choiceCaller struct {
	allowed atomic.Bool
	other   tenancy.Tenant
}

func (c *choiceCaller) ByHost(_ context.Context, _ db.Tx[db.System], h string) (tenancy.Tenant, error) {
	if h == host {
		return acme, nil
	}
	return c.other, nil
}
func (c *choiceCaller) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return c.allowed.Load(), nil
}

func choiceWorld(t *testing.T) (http.Handler, *sql.DB, *choiceCaller) {
	t.Helper()
	caller := &choiceCaller{other: tenancy.Tenant{ID: uuid.New(), Slug: "other"}}
	caller.allowed.Store(true)
	s := rest.Spec[*choiceRecord]{Module: "shapes", Entity: "selection", Path: "/selections", Read: "shape:read", Write: "shape:write", Choices: &entity.ChoiceFields{Fields: []entity.ChoiceField{{Name: "shapes", Choices: entity.ChoiceSet{Cardinality: "many", Options: []entity.ChoiceOption{{ID: "round", Label: "Round"}, {ID: "square", Label: "Square"}, {ID: "triangle", Label: "Triangle"}}, Empty: entity.ChoiceEmpty{Mode: "refuse", Message: "Keep a shape."}, Maximum: &entity.ChoiceMaximum{Count: 2, Behaviour: "replace-oldest"}}}}}}
	_, handler, admin := mountAs(t, s, caller)
	_, err := admin.ExecContext(t.Context(), `CREATE TABLE rest_choice_records (
 id uuid PRIMARY KEY, tenant_id uuid NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL, deleted_at timestamptz,
 shapes jsonb NOT NULL, revision bigint NOT NULL DEFAULT 1);
 ALTER TABLE rest_choice_records ENABLE ROW LEVEL SECURITY;
 ALTER TABLE rest_choice_records FORCE ROW LEVEL SECURITY;
 CREATE POLICY choice_tenant ON rest_choice_records USING (platformkit_tenant_match(tenant_id)) WITH CHECK (platformkit_tenant_match(tenant_id));`)
	if err != nil {
		t.Fatal(err)
	}
	return handler, admin, caller
}

func choiceRequest(handler http.Handler, method, hostname, path, body, tag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+hostname+path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", tag)
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, req)
	return out
}

func choiceCounts(t *testing.T, admin *sql.DB, id string) (string, int64, int) {
	t.Helper()
	var shapes string
	var revision int64
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT shapes::text, revision FROM rest_choice_records WHERE id=$1", id).Scan(&shapes, &revision); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&count); err != nil {
		t.Fatal(err)
	}
	return shapes, revision, count
}

func TestChoiceWritesRecheckTenantRevisionAndEmptyInvariant(t *testing.T) {
	handler, admin, caller := choiceWorld(t)
	const base = "/api/v1/shapes/selections"
	bad := choiceRequest(handler, "POST", host, base, `{"shapes":null}`, "")
	if bad.Code != 422 {
		t.Fatalf("null create: %d %s", bad.Code, bad.Body)
	}
	created := choiceRequest(handler, "POST", host, base, `{"shapes":["round"]}`, "")
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	record := id(t, created.Body.String())
	path := base + "/" + record
	before, rev, events := choiceCounts(t, admin, record)
	for _, tc := range []struct {
		name, hostname, body, tag string
		status                    int
	}{
		{"last removal", host, `{"shapes":[]}`, `"1"`, 422},
		{"no revision", host, `{"shapes":["square"]}`, "", 422},
		{"stale no-op", host, `{"shapes":["round"]}`, `"9"`, 412},
		{"unknown before truncation", host, `{"shapes":["missing","round","square"]}`, `"1"`, 422},
		{"foreign write", "other.test", `{"shapes":["square"]}`, `"1"`, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := choiceRequest(handler, "PATCH", tc.hostname, path, tc.body, tc.tag)
			if out.Code != tc.status {
				t.Fatalf("%d %s", out.Code, out.Body)
			}
			if strings.Contains(out.Body.String(), `"shapes":[`) {
				t.Fatal("refusal leaked a row")
			}
		})
	}
	if out := choiceRequest(handler, "GET", "other.test", path, "", ""); out.Code != 404 {
		t.Fatalf("foreign read: %d", out.Code)
	}
	caller.allowed.Store(false)
	if out := choiceRequest(handler, "PATCH", host, path, `{"shapes":["square"]}`, `"1"`); out.Code != 403 {
		t.Fatalf("revoked grant: %d %s", out.Code, out.Body)
	}
	caller.allowed.Store(true)
	after, gotRev, gotEvents := choiceCounts(t, admin, record)
	if before != after || rev != gotRev || events != gotEvents {
		t.Fatalf("refusal wrote: %s/%d/%d -> %s/%d/%d", before, rev, events, after, gotRev, gotEvents)
	}
	out := choiceRequest(handler, "PATCH", host, path, `{"shapes":["round","square","triangle"]}`, `"1"`)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"shapes":["square","triangle"]`) {
		t.Fatalf("replacement: %d %s", out.Code, out.Body)
	}
	_, gotRev, gotEvents = choiceCounts(t, admin, record)
	if gotRev != 2 || gotEvents != events+1 {
		t.Fatalf("revision/outbox: %d/%d", gotRev, gotEvents)
	}
}

func TestConcurrentChoiceWritesSettleOnOneRevision(t *testing.T) {
	handler, admin, _ := choiceWorld(t)
	const base = "/api/v1/shapes/selections"
	created := choiceRequest(handler, "POST", host, base, `{"shapes":["round"]}`, "")
	if created.Code != 201 {
		t.Fatalf("create: %d %s", created.Code, created.Body)
	}
	record := id(t, created.Body.String())
	_, _, events := choiceCounts(t, admin, record)
	start := make(chan struct{})
	results := make(chan int, 2)
	var workers sync.WaitGroup
	for _, shape := range []string{"square", "triangle"} {
		workers.Go(func() {
			<-start
			out := choiceRequest(handler, "PATCH", host, base+"/"+record, `{"shapes":["`+shape+`"]}`, `"1"`)
			results <- out.Code
		})
	}
	close(start)
	workers.Wait()
	close(results)
	codes := []int{}
	for code := range results {
		codes = append(codes, code)
	}
	slices.Sort(codes)
	if !slices.Equal(codes, []int{200, 412}) {
		t.Fatalf("competing writes: %v", codes)
	}
	_, rev, gotEvents := choiceCounts(t, admin, record)
	if rev != 2 || gotEvents != events+1 {
		t.Fatalf("revision/outbox: %d/%d", rev, gotEvents)
	}
}
