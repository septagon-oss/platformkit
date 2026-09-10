package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
)

// A hidden navigation link is not authorization. Without a product-supplied
// gallery, customer tenants must not receive the installation's source examples.
func TestDefaultGalleryRefusesCustomerTenantByDirectURL(t *testing.T) {
	router := mount(t)
	for _, path := range []string{"/admin/_gallery", "/admin/_gallery?group=Status", "/admin/_gallery/preview?example=pk-ui.component.button/primary", "/admin/_gallery/export"} {
		status, body, _ := callAt(t, router, host, http.MethodGet, path, "")
		if status != http.StatusForbidden {
			t.Errorf("customer gallery %s: status %d, want 403", path, status)
		}
		if strings.Contains(body, "pk-ui.component.") {
			t.Errorf("customer gallery %s leaks source examples", path)
		}
	}
	status, body, _ := callAt(t, router, operatorHost, http.MethodGet, "/admin/_gallery", "")
	if status != http.StatusOK || !strings.Contains(body, "pk-ui.component.") {
		t.Fatal("the operator tenant lost its installation gallery")
	}
}

func TestStorybooksUseOnlyTheAuthorizedTenantComposition(t *testing.T) {
	provider := func(ctx context.Context) (ui.Storybook, error) {
		tenant, _ := tenancy.FromContext(ctx)
		principal, ok := tenancy.PrincipalFrom(ctx)
		if !ok || len(principal.Roles) == 0 {
			t.Fatal("provider did not receive the authenticated principal")
		}
		name, color := "Acme only", "#123456"
		if tenant.Operator {
			name, color = "Operator only", "#654321"
		}
		pair := design.Default()
		pair.Light.AccentDefault = color
		return ui.Storybook{Title: name, Theme: pair, Examples: []c.Example{
			c.ExampleOf(c.ExampleInfo{ID: name, ComponentID: "product.button", Group: "Product", Name: name}, c.ButtonProps{Label: name}, c.Button),
		}}, nil
	}
	router := mountAs(t, caller{}, func(d *admin.Deps) { d.Storybook = provider })
	for _, test := range []struct{ at, own, other, color, otherColor string }{
		{host, "Acme only", "Operator only", "#123456", "#654321"},
		{operatorHost, "Operator only", "Acme only", "#654321", "#123456"},
	} {
		for _, path := range []string{"/admin/_gallery", "/admin/_gallery/preview?example=" + url.QueryEscape(test.own), "/admin/_gallery/export"} {
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Host = test.at
			r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
			out := httptest.NewRecorder()
			router.ServeHTTP(out, r)
			body := out.Body.String()
			if out.Code != http.StatusOK || !strings.Contains(body, test.own) || strings.Contains(body, test.other) || strings.Contains(body, "pk-ui.component.") || strings.Contains(body, test.otherColor) {
				t.Errorf("%s at %s leaks or loses its composition: status %d", path, test.at, out.Code)
			}
			if out.Header().Get("Cache-Control") != "no-store" {
				t.Errorf("%s can be cached", path)
			}
			if strings.Contains(path, "preview") {
				policy := out.Header().Get("Content-Security-Policy")
				if !strings.Contains(policy, "sandbox allow-scripts;") || strings.Contains(policy, "allow-same-origin") || !strings.Contains(policy, "form-action 'none'") || out.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
					t.Errorf("direct preview is not isolated: %s", policy)
				}
				if !strings.Contains(body, test.color) {
					t.Error("preview lost tenant theme")
				}
			}
			if strings.HasSuffix(path, "export") {
				var snapshot ui.DesignExport
				if err := json.Unmarshal(out.Body.Bytes(), &snapshot); err != nil || len(snapshot.Examples) != 1 || snapshot.Examples[0].ID != test.own {
					t.Error("export contains a different catalog")
				}
			}
		}
		for _, route := range []string{"/admin/_gallery", "/admin/_gallery/preview"} {
			status, _, _ := callAt(t, router, test.at, http.MethodGet, route+"?example="+url.QueryEscape(test.other)+"&tenant="+url.QueryEscape(test.other), "")
			if status != http.StatusNotFound {
				t.Errorf("direct other-tenant ID at %s = %d", route, status)
			}
		}
	}
}

func TestStorybookDenialAndEmptyCompositionNeverFallBack(t *testing.T) {
	for _, denied := range []bool{false, true} {
		router := mountAs(t, caller{}, func(d *admin.Deps) {
			d.Storybook = func(context.Context) (ui.Storybook, error) {
				if denied {
					return ui.Storybook{}, problem.New(http.StatusForbidden, "No access")
				}
				return ui.Storybook{Title: "Empty product"}, nil
			}
		})
		for _, path := range []string{"/admin/_gallery", "/admin/_gallery/export"} {
			status, body, _ := callAt(t, router, operatorHost, http.MethodGet, path, "")
			want := http.StatusOK
			if denied {
				want = http.StatusForbidden
			}
			if status != want || strings.Contains(body, "pk-ui.component.") {
				t.Errorf("denied=%v %s = %d", denied, path, status)
			}
		}
	}
}

func TestStorybookRequiresPermissionBeforeCallingProvider(t *testing.T) {
	router := mountAs(t, member{}, func(d *admin.Deps) {
		d.Storybook = func(context.Context) (ui.Storybook, error) {
			t.Error("unauthorized provider call")
			return ui.Storybook{}, nil
		}
	})
	for _, path := range []string{"/admin/_gallery", "/admin/_gallery/preview?example=private", "/admin/_gallery/export"} {
		status, _, _ := call(t, router, http.MethodGet, path, "")
		if status != http.StatusForbidden {
			t.Errorf("%s = %d, want 403", path, status)
		}
	}
}
