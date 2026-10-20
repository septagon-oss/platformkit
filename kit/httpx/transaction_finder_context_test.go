package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestHTTPTransactionFinderKeepsItsCallerContextWithoutCancellingTheRequest(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)
	type callerKey struct{}
	const event = "probe.noted"
	called := false
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "caller-note", Method: http.MethodPost, Path: "/caller-note",
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		called = true
		if _, ok := httpx.TxFrom(ctx); !ok {
			return nil, errors.New("the request could not open its transaction")
		}
		caller, cancel := context.WithTimeout(context.WithValue(ctx, callerKey{}, "finder"), time.Minute)
		defer cancel()
		tx, ok := httpx.TxFrom(caller)
		if !ok {
			return nil, errors.New("the caller could not find the opened transaction")
		}
		statement := tx.DB().Statement.Context
		if got := statement.Value(callerKey{}); got != "finder" {
			t.Errorf("statement value = %v, want finder", got)
		}
		want, _ := caller.Deadline()
		if got, ok := statement.Deadline(); !ok || !got.Equal(want) {
			t.Errorf("statement deadline = %v (%v), want %v", got, ok, want)
		}
		cancel()
		if err := tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, 'cancelled')", f.tenant.ID).Error; !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled caller's write = %v, want context.Canceled", err)
		}
		if err := events.Publish(caller, tx, event, struct{}{}); !errors.Is(err, context.Canceled) {
			t.Errorf("cancelled caller's event = %v, want context.Canceled", err)
		}
		live, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, errors.New("the live caller lost the request's transaction")
		}
		if got := live.DB().Statement.Context.Value(callerKey{}); got != nil {
			t.Errorf("the live caller inherited another caller's value: %v", got)
		}
		if err := live.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, 'live')", f.tenant.ID).Error; err != nil {
			return nil, err
		}
		if err := events.Publish(ctx, live, event, struct{}{}); err != nil {
			return nil, err
		}
		return &body{}, nil
	})
	w := request(t, router, http.MethodPost, at(api, "/caller-note"), host)
	if !called || w.Code != http.StatusOK {
		t.Fatalf("handler called = %v, response = %d %s", called, w.Code, w.Body.String())
	}
	if n := notes(t, f); n != 1 {
		t.Errorf("committed %d notes, want only the live caller's", n)
	}
	var emitted int
	err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = ?", event).Row().Scan(&emitted)
	})
	if err != nil {
		t.Fatal(err)
	}
	if emitted != 1 {
		t.Errorf("committed %d events, want only the live caller's", emitted)
	}
}
