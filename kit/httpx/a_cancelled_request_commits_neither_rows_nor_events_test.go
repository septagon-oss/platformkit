package httpx_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The request's context owns its transaction: a request cancelled after its
// handler wrote and decided 201 keeps neither the row nor the event.
func TestARequestCancelledBeforeItsCommitKeepsNeitherRowsNorEvents(t *testing.T) {
	api, router, f := setup(t)
	f.exec(`CREATE TABLE notes (id serial PRIMARY KEY, tenant_id uuid NOT NULL, body text NOT NULL)`)

	const event = "probe.noted"
	ctx, cancel := context.WithCancel(t.Context())
	var wrote error
	httpx.Register(api.Surfaces(probe).App, huma.Operation{
		OperationID: "cancelled-note", Method: http.MethodPost, Path: "/cancelled", DefaultStatus: http.StatusCreated,
	}, httpx.Public(), func(ctx context.Context, _ *struct{}) (*body, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, errors.New("the request had no transaction")
		}
		wrote = tx.DB().Exec("INSERT INTO notes (tenant_id, body) VALUES (?, 'note')", db.TenantOf(tx).ID).Error
		if wrote == nil {
			wrote = events.Publish(ctx, tx, event, map[string]any{})
		}
		cancel()
		return &body{}, nil
	})

	req := httptest.NewRequest(http.MethodPost, "http://"+host+at(api, "/cancelled"), nil).WithContext(ctx)
	router.ServeHTTP(httptest.NewRecorder(), req)

	if wrote != nil {
		t.Fatalf("the handler's writes failed before the cancel: %v", wrote)
	}
	got := f.logs.String()
	if !strings.Contains(got, "the client hung up before the request transaction ended") {
		t.Errorf("the cancelled request was not reported as a hang-up:\n%s", got)
	}
	for _, line := range strings.Split(strings.TrimSpace(got), "\n") {
		if strings.Contains(line, "level=ERROR") {
			t.Errorf("a cancelled request logged an error: %s", line)
		}
	}
	if n := notes(t, f); n != 0 {
		t.Errorf("the cancelled request kept %d row(s)", n)
	}
	var events int
	err := db.Run(tenancy.WithTenant(t.Context(), f.tenant), f.app, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = ?", event).Row().Scan(&events)
	})
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	if events != 0 {
		t.Errorf("the cancelled request kept %d event(s)", events)
	}
}
