package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	requestmeta "github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAdmittedWritesCarryTheirReceiptAndFinalOutcome(t *testing.T) {
	for _, mode := range []string{"succeeded", "failed", "pending_approval", "async", "panic"} {
		t.Run(mode, func(t *testing.T) {
			_, _, f := setup(t)
			f.signedIn()
			f.allow = true
			at := time.Date(2026, 10, 14, 4, 0, 0, 0, time.UTC)
			api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), Tenants: f, Conn: f.app, Authorize: f, Authenticate: f.authenticate,
				Idempotency: idempotency.Postgres(httpx.ConnFrom), Operations: idempotency.PostgresOperations(), Clock: func() time.Time { return at }})
			runs := 0
			httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "write-operation", Method: "POST", Path: "/operation", DefaultStatus: 201}, httpx.SignedIn(),
				func(ctx context.Context, _ *struct{}) (*body, error) {
					runs++
					tx, ok := httpx.TxFrom(ctx)
					if !ok {
						return nil, errors.New("no transaction")
					}
					op, present := requestmeta.OperationFrom(ctx)
					if !present || op.ID != "phone.queue.write" || op.Person != f.principal.UserID.String() {
						t.Errorf("admission = %+v present %v", op, present)
					}
					if mode == "pending_approval" {
						if err := events.RecordOutcome(ctx, tx, op.Revision, requestmeta.PendingApproval, at); err != nil {
							return nil, err
						}
						requestmeta.DeferOperation(ctx)
					}
					if mode == "async" {
						requestmeta.DeferOperation(ctx)
					}
					if mode == "succeeded" {
						if err := events.Succeed(ctx, tx, at); err != nil {
							return nil, err
						}
					}
					if err := events.Publish(ctx, tx, "probe.accepted", struct{}{}); err != nil {
						return nil, err
					}
					if mode == "failed" {
						return nil, huma.Error422UnprocessableEntity("save refused")
					}
					if mode == "panic" {
						panic("refused by panic")
					}
					return &body{}, nil
				})
			authedRouter := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("Authorization", "Bearer present")
				router.ServeHTTP(w, r)
			})
			first := keyedPost(t, authedRouter, "POST", atPath(api), "phone.queue.write", `{}`)
			want := 201
			if mode == "failed" {
				want = 422
			}
			if mode == "panic" {
				want = 500
			}
			if first.Code != want {
				t.Fatalf("first = %d %s, logs %s", first.Code, first.Body.String(), f.logs.String())
			}
			id, location := first.Header().Get(httpx.OperationIDHeader), first.Header().Get(httpx.OperationLocationHeader)
			if id != "phone.queue.write" || location == "" {
				t.Fatalf("headers = %v", first.Header())
			}
			retry := keyedPost(t, authedRouter, "POST", atPath(api), "phone.queue.write", `{}`)
			if retry.Code != first.Code || retry.Body.String() != first.Body.String() || retry.Header().Get(httpx.OperationLocationHeader) != location || runs != 1 {
				t.Errorf("retry = %d %s runs=%d", retry.Code, retry.Body.String(), runs)
			}
			ref, err := uuid.Parse(location[len("/api/v1/app/operations/me/"):])
			if err != nil {
				t.Fatal(err)
			}
			ctx := tenancy.WithTenant(t.Context(), f.tenant)
			if err := db.Run(ctx, f.app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				r, err := idempotency.PostgresOperations().Get(ctx, tx, ref, f.principal.UserID, at)
				if err != nil {
					return err
				}
				outcome := requestmeta.Outcome(mode)
				if mode == "panic" {
					outcome = requestmeta.Failed
				}
				if mode == "async" {
					outcome = ""
				}
				if r.Outcome != outcome {
					t.Errorf("receipt = %+v, want %q", r, outcome)
				}
				var domain, evidence int
				if err := tx.DB().Raw(`SELECT count(*) FILTER (WHERE name='probe.accepted'),count(*) FILTER (WHERE name='platformkit.operation_outcome') FROM platformkit_outbox WHERE operation_ref=?`, ref).Row().Scan(&domain, &evidence); err != nil {
					return err
				}
				if outcome == requestmeta.Failed {
					if domain != 0 || evidence != 1 {
						t.Errorf("refused write left domain=%d evidence=%d", domain, evidence)
					}
				} else if outcome == "" {
					if domain != 1 || evidence != 0 {
						t.Errorf("async left domain=%d evidence=%d", domain, evidence)
					}
				} else if domain != 1 || evidence != 1 {
					t.Errorf("write left domain=%d evidence=%d", domain, evidence)
				}
				if mode == "async" {
					ctx = requestmeta.WithOperation(ctx, r.Operation())
					return events.RecordOutcome(ctx, tx, r.Revision, requestmeta.Succeeded, at.Add(time.Minute))
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func atPath(api *httpx.API) string { return api.Surfaces(probe).App.Path("/operation") }

func TestEveryUnsafeVerbGetsIdentityAndCredentialsAreNeverReplayed(t *testing.T) {
	_, _, f := setup(t)
	f.signedIn()
	f.allow = true
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), Tenants: f, Conn: f.app, Authorize: f, Authenticate: f.authenticate,
		Idempotency: idempotency.Postgres(httpx.ConnFrom), Operations: idempotency.PostgresOperations()})
	for _, method := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "verb-" + method, Method: method, Path: "/verb"}, httpx.SignedIn(), ok)
	}
	runs := 0
	extension, value := httpx.MintsACredential()
	httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "credential-operation", Method: "POST", Path: "/credential", Extensions: map[string]any{extension: value}}, httpx.SignedIn(), func(context.Context, *struct{}) (*body, error) { runs++; return &body{}, nil })
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer present")
		router.ServeHTTP(w, r)
	})
	seen := map[string]bool{}
	for _, method := range []string{"POST", "PATCH", "PUT", "DELETE"} {
		res := keyedPost(t, authed, method, api.Surfaces(probe).App.Path("/verb"), "", `{}`)
		id := res.Header().Get(httpx.OperationIDHeader)
		if res.Code != 200 || id == "" || seen[id] || res.Header().Get(httpx.OperationLocationHeader) == "" {
			t.Errorf("%s = %d headers %v", method, res.Code, res.Header())
		}
		seen[id] = true
	}
	path := api.Surfaces(probe).App.Path("/credential")
	first := keyedPost(t, authed, "POST", path, "mint.once", `{}`)
	retry := keyedPost(t, authed, "POST", path, "mint.once", `{}`)
	if first.Code != 200 || retry.Code != 409 || runs != 1 || first.Header().Get(httpx.OperationLocationHeader) != retry.Header().Get(httpx.OperationLocationHeader) {
		t.Errorf("credential first=%d retry=%d runs=%d", first.Code, retry.Code, runs)
	}
}

func TestMutationCannotFlushBeforeCommit(t *testing.T) {
	_, _, f := setup(t)
	f.signedIn()
	f.allow = true
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), Tenants: f, Conn: f.app, Authorize: f, Authenticate: f.authenticate,
		Idempotency: idempotency.Postgres(httpx.ConnFrom), Operations: idempotency.PostgresOperations()})
	httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "streamed-write", Method: "POST", Path: "/stream"}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*huma.StreamResponse, error) {
		tx, _ := httpx.TxFrom(ctx)
		if err := events.Publish(ctx, tx, "probe.streamed", struct{}{}); err != nil {
			return nil, err
		}
		return &huma.StreamResponse{Body: func(ctx huma.Context) {
			writer := ctx.BodyWriter()
			_, _ = writer.Write([]byte("uncommitted"))
			if f, ok := writer.(http.Flusher); ok {
				f.Flush()
			}
		}}, nil
	})
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer present")
		router.ServeHTTP(w, r)
	})
	res := keyedPost(t, authed, "POST", api.Surfaces(probe).App.Path("/stream"), "stream.once", `{}`)
	if res.Code != 500 || strings.Contains(res.Body.String(), "uncommitted") || res.Header().Get(httpx.OperationLocationHeader) == "" {
		t.Errorf("stream = %d %s headers %v", res.Code, res.Body.String(), res.Header())
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var domains, failed int
		if err := tx.DB().Raw(`SELECT count(*) FILTER (WHERE name='probe.streamed'),count(*) FILTER (WHERE name='platformkit.operation_outcome' AND operation_outcome='failed') FROM platformkit_outbox`).Row().Scan(&domains, &failed); err != nil {
			return err
		}
		if domains != 0 || failed != 1 {
			t.Errorf("domains=%d failures=%d", domains, failed)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

type unavailableOutcomes struct{ idempotency.Operations }

func (unavailableOutcomes) Current(context.Context, db.Tx[db.Tenant], requestmeta.Operation) (idempotency.Receipt, error) {
	return idempotency.Receipt{}, errors.New("outcome evidence unavailable")
}

func TestAPanicWhoseFailureEvidenceCannotPersistReturnsUnavailable(t *testing.T) {
	_, _, f := setup(t)
	f.signedIn()
	f.allow = true
	api, router := httpx.New(httpx.Options{Cache: cache.Memory("pkit"), Tenants: f, Conn: f.app, Authorize: f, Authenticate: f.authenticate,
		Idempotency: idempotency.Postgres(httpx.ConnFrom), Operations: unavailableOutcomes{idempotency.PostgresOperations()}})
	httpx.Register(api.Surfaces(probe).App, huma.Operation{OperationID: "panic-no-evidence", Method: "POST", Path: "/panic"}, httpx.SignedIn(), func(context.Context, *struct{}) (*body, error) { panic("refused") })
	authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Authorization", "Bearer present")
		router.ServeHTTP(w, r)
	})
	res := keyedPost(t, authed, "POST", api.Surfaces(probe).App.Path("/panic"), "panic.once", `{}`)
	if res.Code != 503 || res.Header().Get(httpx.OperationLocationHeader) == "" {
		t.Errorf("panic = %d %s headers %v", res.Code, res.Body.String(), res.Header())
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var unknown bool
		var emitted int
		if err := tx.DB().Raw(`SELECT outcome IS NULL FROM platformkit_operations WHERE operation_id='panic.once'`).Row().Scan(&unknown); err != nil {
			return err
		}
		if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_outbox`).Row().Scan(&emitted); err != nil {
			return err
		}
		if !unknown || emitted != 0 {
			t.Errorf("unknown=%v emitted=%d", unknown, emitted)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
