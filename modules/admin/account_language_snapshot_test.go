package admin_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"golang.org/x/net/html"
	"gorm.io/gorm"
)

type countedAccountUsers struct{ accountUsers }

func (a countedAccountUsers) Revision(ctx context.Context, tx db.Tx[db.Tenant]) (int64, error) {
	held, err := a.OwnLanguage(ctx, tx)
	return held.Revision, err
}

func (a countedAccountUsers) ChooseAt(ctx context.Context, tx db.Tx[db.Tenant], tag string, revision int64) error {
	_, err := a.SetPreferredLocaleAt(ctx, tx, revision, tag)
	return err
}

func TestAccountLanguageFormCannotCombineDifferentRevisions(t *testing.T) {
	owner, conn := dbtest.Schema(t, user.Migrations)
	tenants, _ := tenant.New(tenant.Deps{
		Languages: []string{"en", "pt-PT"}, LocaleManager: tenanttest.NewFake(),
	})
	users, _ := user.New(user.Deps{
		LocalePolicy: tenants,
		Administration: &usercontracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) {
			return []string{"admin"}, nil
		}},
		Granting: &usercontracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) {
			return true, nil
		}},
	})
	var tenantRow *tenantcontracts.Tenant
	var person *usercontracts.User
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenantRow, err = tenants.Create(ctx, tx, tenantcontracts.NewTenant{Slug: "language", Name: "Language", Host: host})
		if err != nil {
			return err
		}
		tenantRow, err = tenants.SetLocale(ctx, tx, tenantRow.ID, tenantcontracts.SetLocale{Default: "en", Supported: []string{"pt-PT"}})
		if err != nil {
			return err
		}
		person, err = users.Provision(ctx, tx, tenantRow.ID, "reader@language.test", "Reader", "fixture-password", []string{"admin"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	actor := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenantRow.Tenancy()), tenancy.Principal{UserID: person.ID})
	if err := db.Run(actor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := users.SetPreferredLocaleAt(ctx, tx, 0, "en")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: tenants, Conn: conn, Authorize: caller{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: person.ID, Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	account := countedAccountUsers{accountUsers{users}}
	shell := admin.New(admin.Deps{SignIn: "/api/v1/auth/login", Authorize: caller{}, Language: account,
		LanguageVersion: account, Messages: xtext.Load("en", admin.Catalogue())})
	var grants []tenancy.Grant
	for _, permission := range shell.Permissions {
		grants = append(grants, tenancy.Grant{Permission: permission.Key, Operator: permission.Operator})
	}
	api.Declare(grants)
	shell.Routes(api.Surfaces(shell.Name))
	if err := module.Validate([]module.Module{shell}); err != nil {
		t.Fatal(err)
	}
	var intervened atomic.Bool
	if err := db.Run(actor, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		queries := tx.DB().Callback().Query()
		if err := queries.After("gorm:query").Register("account_choice_read", func(query *gorm.DB) {
			row, ok := query.Statement.Dest.(*usercontracts.User)
			if !ok || row.ID != person.ID || !intervened.CompareAndSwap(false, true) {
				return
			}
			// A second tab commits after the preference read, before the frame finishes.
			if err := db.Run(actor, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := users.SetPreferredLocaleAt(ctx, tx, 1, "pt-PT")
				return err
			}); err != nil {
				t.Errorf("intervening language choice: %v", err)
			}
		}); err != nil {
			return err
		}
		t.Cleanup(func() { _ = queries.Remove("account_choice_read") })
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	w := request(http.MethodGet, "/app", "")
	if w.Code != http.StatusOK || !intervened.Load() {
		t.Fatalf("account page = %d; intervening committed choice reached = %v", w.Code, intervened.Load())
	}
	root, err := html.Parse(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	form, action := url.Values{}, ""
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, account bool) {
		attrs := map[string]string{}
		for _, attr := range n.Attr {
			attrs[attr.Key] = attr.Val
		}
		if n.Data == "form" {
			_, account = attrs["data-account-language"]
			if account {
				action = attrs["action"]
			}
		}
		if account && n.Data == "input" && attrs["name"] != "" {
			form.Set(attrs["name"], attrs["value"])
		}
		if _, selected := attrs["selected"]; account && n.Data == "option" && selected {
			form.Set("locale", attrs["value"])
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, account)
		}
	}
	walk(root, false)
	if action == "" || !form.Has("locale") || !form.Has("expectedRevision") {
		t.Fatalf("no counted account form: action=%q values=%v", action, form)
	}
	var before int
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox WHERE name = 'user.language_set'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w = request(http.MethodPost, action, form.Encode())
	var language string
	var revision int64
	var after int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT preferred_locale, locale_revision, (SELECT count(*) FROM platformkit_outbox WHERE name = 'user.language_set') FROM users WHERE id = $1`, person.ID).
		Scan(&language, &revision, &after); err != nil {
		t.Fatal(err)
	}
	if language != "pt-PT" || revision != 2 || after != before {
		t.Errorf("unchanged form %v returned %d and replaced the intervening choice: language=%q revision=%d events=%d (before %d); want pt-PT at revision 2 without a new event",
			form, w.Code, language, revision, after, before)
	}
}
