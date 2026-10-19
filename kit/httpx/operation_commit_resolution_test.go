package httpx_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	requestmeta "github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTrackedCommandResolvesItsCommitWithoutChangingItsOccurrence(t *testing.T) {
	for _, test := range []struct {
		fault    string
		deferred bool
	}{
		{"lost-acknowledgement", false}, {"cancelled-acknowledgement", false}, {"refused-commit", false},
		{"lost-acknowledgement", true}, {"cancelled-acknowledgement", true}, {"refused-commit", true},
	} {
		name := test.fault
		if test.deferred {
			name += "-deferred"
		}
		t.Run(name, func(t *testing.T) {
			_, _, f := setup(t)
			f.signedIn()
			f.allow = true
			at := time.Date(2026, 10, 14, 4, 0, 0, 0, time.UTC)
			api, router := httpx.New(httpx.Options{
				Cache: cache.Memory("pkit"), Tenants: f, Conn: f.app, Authorize: f, Authenticate: f.authenticate,
				Idempotency: idempotency.Postgres(httpx.ConnFrom), Operations: idempotency.PostgresOperations(),
				Clock: func() time.Time { return at },
			})
			var committed atomic.Pointer[unacknowledgedCommit]
			var refused *refusedCommit
			runs := 0
			rowID := uuid.NewString()
			httpx.Register(api.Surfaces(probe).App, huma.Operation{
				OperationID: "resolve-tracked-commit", Method: http.MethodPost, Path: "/resolve-commit", DefaultStatus: http.StatusCreated,
			}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{ Body keyedOut }, error) {
				runs++
				if test.deferred {
					requestmeta.DeferOperation(ctx)
				}
				tx, ok := httpx.TxFrom(ctx)
				if !ok {
					return nil, errors.New("command has no tenant transaction")
				}
				if err := tx.DB().Exec(`INSERT INTO platformkit_handled (event_id,durable,tenant_id) VALUES (?,?,?)`, uuid.New(), rowID, f.tenant.ID).Error; err != nil {
					return nil, err
				}
				if err := events.Publish(ctx, tx, "probe.accepted", struct{}{}); err != nil {
					return nil, err
				}
				if runs == 1 {
					if test.fault == "refused-commit" {
						pool := tx.DB().Statement.ConnPool
						refused = &refusedCommit{ConnPool: pool, TxCommitter: pool.(gorm.TxCommitter)}
						tx.DB().Statement.ConnPool = refused
					} else {
						ackError := io.ErrUnexpectedEOF
						if test.fault == "cancelled-acknowledgement" {
							ackError = context.Canceled
						}
						tx.DB().Statement.ConnPool = lostAcknowledgement(ackError, &committed)(tx)
					}
				}
				return &struct{ Body keyedOut }{Body: keyedOut{ID: rowID}}, nil
			})
			authed := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("Authorization", "Bearer present")
				router.ServeHTTP(w, r)
			})
			path := api.Surfaces(probe).App.Path("/resolve-commit")
			first := keyedPost(t, authed, http.MethodPost, path, "tracked.commit", `{}`)
			location := first.Header().Get(httpx.OperationLocationHeader)
			ref, err := uuid.Parse(strings.TrimPrefix(location, "/api/v1/app/operations/me/"))
			if err != nil {
				t.Fatalf("admitted occurrence location = %q: %v", location, err)
			}
			wantRows := 1
			wantOutcome := requestmeta.Succeeded
			if test.deferred {
				wantOutcome = ""
			}
			if test.fault == "refused-commit" {
				if refused == nil || !refused.rolledBack.Load() {
					t.Fatal("commit fault did not reach a real rollback")
				}
				wantRows, wantOutcome = 0, ""
			} else {
				committedAnyway(t, &committed)
			}
			check := func(wantRows int, wantOutcome requestmeta.Outcome) {
				t.Helper()
				if err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					receipt, err := idempotency.PostgresOperations().Get(ctx, tx, ref, f.principal.UserID, at)
					if err != nil {
						return err
					}
					if receipt.Outcome != wantOutcome {
						t.Errorf("receipt outcome = %q, want %q", receipt.Outcome, wantOutcome)
					}
					var rows, domain, outcomes int
					if err := tx.DB().Raw(`SELECT count(*) FROM platformkit_handled WHERE durable=?`, rowID).Row().Scan(&rows); err != nil {
						return err
					}
					if err := tx.DB().Raw(`SELECT count(*) FILTER (WHERE name='probe.accepted'), count(*) FILTER (WHERE name='platformkit.operation_outcome') FROM platformkit_outbox WHERE operation_ref=?`, ref).Row().Scan(&domain, &outcomes); err != nil {
						return err
					}
					wantOutcomes := wantRows
					if wantOutcome == "" {
						wantOutcomes = 0
					}
					if rows != wantRows || domain != wantRows || outcomes != wantOutcomes {
						t.Errorf("committed rows=%d domain events=%d outcome events=%d, want %d / %d / %d", rows, domain, outcomes, wantRows, wantRows, wantOutcomes)
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			check(wantRows, wantOutcome)
			if test.fault != "cancelled-acknowledgement" && (first.Code != http.StatusInternalServerError || strings.Contains(first.Body.String(), rowID)) {
				t.Errorf("unacknowledged command response = %d %s, want 500 without the command row", first.Code, first.Body.String())
			}
			at = at.Add(idempotency.Lease + time.Second)
			retry := keyedPost(t, authed, http.MethodPost, path, "tracked.commit", `{}`)
			wantStatus, wantRuns := http.StatusConflict, 1
			if test.fault == "refused-commit" {
				wantStatus, wantRuns = http.StatusCreated, 2
			} else if !strings.Contains(retry.Body.String(), httpx.CodeIdempotencySpent) || strings.Contains(retry.Body.String(), rowID) {
				t.Errorf("committed command retry = %s, want spent reason without a stale command row", retry.Body.String())
			}
			if retry.Code != wantStatus || runs != wantRuns || retry.Header().Get(httpx.OperationLocationHeader) != location {
				t.Errorf("retry status=%d runs=%d location=%q, want %d / %d / %q; body=%s", retry.Code, runs, retry.Header().Get(httpx.OperationLocationHeader), wantStatus, wantRuns, location, retry.Body.String())
			}
			settled := requestmeta.Succeeded
			if test.deferred {
				settled = ""
			}
			check(1, settled)
			last := keyedPost(t, authed, http.MethodPost, path, "tracked.commit", `{}`)
			if last.Code != retry.Code || runs != wantRuns || last.Header().Get(httpx.OperationLocationHeader) != location {
				t.Errorf("settled retry changed: status=%d runs=%d body=%s", last.Code, runs, last.Body.String())
			}
			if test.fault == "refused-commit" {
				if last.Body.String() != retry.Body.String() {
					t.Errorf("recorded success did not replay: %s", last.Body.String())
				}
			} else if !strings.Contains(last.Body.String(), httpx.CodeIdempotencySpent) || strings.Contains(last.Body.String(), rowID) {
				t.Errorf("settled refusal = %s, want spent reason without a stale command row", last.Body.String())
			}
		})
	}
}
