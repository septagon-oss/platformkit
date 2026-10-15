package httpx_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnInflightHostLookupCannotRestoreRemovedTenantLanguages(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	const alias = "alias.acme.test"
	tenantID := uuid.New()
	if _, err := owner.ExecContext(ctx,
		"INSERT INTO tenants (id, slug, name) VALUES ($1, 'acme', 'Acme')", tenantID); err != nil {
		t.Fatal(err)
	}
	snapshotTaken := make(chan struct{})
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var held atomic.Bool

	api, router := httpx.New(httpx.Options{
		Conn: conn, Installation: host, PublicHost: host,
		Tenants: loaderFunc(func(ctx context.Context, tx db.Tx[db.System], h string) (tenancy.Tenant, error) {
			if h != host && h != alias {
				return tenancy.Tenant{}, tenancy.ErrNoSuchHost
			}
			var tag string
			if err := tx.DB().Raw("SELECT default_locale FROM tenants WHERE id = ?", tenantID).Row().Scan(&tag); err != nil {
				return tenancy.Tenant{}, err
			}
			snapshot := tenancy.Tenant{ID: tenantID, Slug: "acme", Languages: &tenancy.Languages{Default: tag}}
			if h == alias && held.CompareAndSwap(false, true) {
				close(snapshotTaken)
				select {
				case <-release:
				case <-ctx.Done():
					return tenancy.Tenant{}, ctx.Err()
				}
			}
			return snapshot, nil
		}),
		Authorize: authorizerFunc(func(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
			return true, nil
		}),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	surface := api.Surfaces("tenant")
	httpx.Register(surface.App, huma.Operation{
		OperationID: "tenant-current-language", Method: http.MethodGet, Path: "/current",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*body, error) {
		tenant, _ := tenancy.FromContext(ctx)
		out := new(body)
		out.Body.Tenant = tenant.Languages.Default
		return out, nil
	})
	httpx.Register(surface.App, huma.Operation{
		OperationID: "tenant-replace-languages", Method: http.MethodPut, Path: "/languages",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*body, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, fmt.Errorf("no tenant transaction")
		}
		if err := tx.DB().Exec("UPDATE tenants SET default_locale = 'pt-PT', locale_revision = locale_revision + 1 WHERE id = ?", tenantID).Error; err != nil {
			return nil, err
		}
		httpx.ForgetTenantOnCommit(ctx, tenantID)
		return new(body), nil
	})
	read := func(authority string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+authority+surface.App.Path("/current"), nil).WithContext(ctx)
		req.AddCookie(&http.Cookie{Name: httpx.SessionCookie, Value: "present"})
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	if w := read(host); w.Code != http.StatusOK {
		t.Fatalf("warm primary host = %d: %s", w.Code, w.Body.String())
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- read(alias) }()
	select {
	case <-snapshotTaken:
	case <-ctx.Done():
		t.Fatal("alias lookup did not take its snapshot")
	}
	w := request(t, router, http.MethodPut, surface.App.Path("/languages"), host)
	if w.Code != http.StatusOK {
		t.Fatalf("replace languages = %d: %s", w.Code, w.Body.String())
	}
	var committed string
	if err := owner.QueryRowContext(ctx, "SELECT default_locale FROM tenants WHERE id = $1", tenantID).Scan(&committed); err != nil {
		t.Fatal(err)
	}
	if committed != "pt-PT" {
		t.Fatalf("committed language = %q, want pt-PT", committed)
	}
	if w := read(host); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pt-PT") {
		t.Fatalf("primary host after commit = %d: %s", w.Code, w.Body.String())
	}
	unblock()
	select {
	case w := <-done:
		if w.Code != http.StatusOK {
			t.Fatalf("inflight alias request = %d: %s", w.Code, w.Body.String())
		}
	case <-ctx.Done():
		t.Fatal("alias request did not finish")
	}
	if w := read(alias); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "pt-PT") {
		t.Errorf("a request begun after commit sees removed tenant languages: status=%d body=%s; want pt-PT", w.Code, w.Body.String())
	}
}
