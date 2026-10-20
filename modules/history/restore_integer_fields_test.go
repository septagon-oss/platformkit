package history_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type CountedMemo struct {
	Memo
	Count int64 `json:"count" gorm:"not null;default:0"`
}

func mountedHistorySpec[T crud.Entity](t *testing.T, w *world, spec rest.Spec[T], authorizers ...httpx.Authorizer) (*httpx.API, http.Handler) {
	t.Helper()
	var authorize httpx.Authorizer = grants{memoRead: true, memoWrite: true}
	if len(authorizers) > 0 {
		authorize = authorizers[0]
	}
	api, router := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: acmeHost, Installation: acmeHost,
		Tenants: hosts{byHost: map[string]tenancy.Tenant{acmeHost: acme, globexHost: globex}},
		Conn:    w.app, Authorize: authorize,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	spec.Mount(api.Surfaces(spec.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return api, router
}

func TestRestoringAnIntegerPreservesEveryMinorUnit(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.ExecContext(t.Context(),
		"ALTER TABLE history_memos ADD COLUMN count bigint NOT NULL DEFAULT 0"); err != nil {
		t.Fatal(err)
	}
	_, router := mountedHistorySpec(t, w, rest.Spec[*CountedMemo]{
		Module: "memos", Entity: "memo", Path: "/memo", Name: rest.Name{One: "Memo", Other: "Memos"},
		Read: memoRead, Write: memoWrite,
		History: true, Versions: w.store, Immutable: []string{"status"},
	})
	const original int64 = 9007199254740993
	code, body := call(t, router, acmeHost, http.MethodPost, "/api/v1/memos/memo",
		`{"title":"Exact count","count":9007199254740993}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	var first CountedMemo
	if err := json.Unmarshal([]byte(body), &first); err != nil {
		t.Fatal(err)
	}
	if first.Count != original {
		t.Fatalf("create stored %d, want %d", first.Count, original)
	}
	stored := w.versionsOf(first.ID)
	var document CountedMemo
	if len(stored) != 1 {
		t.Fatalf("create kept %d versions", len(stored))
	}
	if err := json.Unmarshal([]byte(stored[0].Fields), &document); err != nil || document.Count != original {
		t.Fatalf("version 1 lost the original count: %d, %v", document.Count, err)
	}
	if code, body := call(t, router, acmeHost, http.MethodPatch, address(first.ID),
		`{"count":7}`); code != http.StatusOK {
		t.Fatalf("edit = %d %s", code, body)
	}
	if code, body := restore(t, router, acmeHost, first.ID, 1, 2); code != http.StatusOK {
		t.Fatalf("restore = %d %s", code, body)
	}
	var restored int64
	if err := w.admin.QueryRowContext(t.Context(),
		"SELECT count FROM history_memos WHERE id = $1", first.ID).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != original {
		t.Errorf("restore wrote %d, want the stored %d without rounding", restored, original)
	}
}
