package admin_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

// Adapt the published user service to the shell's port, as composition does.
// The shared fake owns the decision and event history.
type accountUsers struct{ usercontracts.Service }

func (a accountUsers) Preferred(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (string, error) {
	u, err := a.Get(ctx, tx, id)
	if err != nil {
		return "", err
	}
	return u.PreferredLocale, nil
}

func (a accountUsers) Choose(ctx context.Context, tx db.Tx[db.Tenant], tag string) error {
	_, err := a.SetPreferredLocale(ctx, tx, tag)
	return err
}

func TestAccountLanguageRefusalSpeaksPortuguese(t *testing.T) {
	users := usertest.NewFake()
	users.ServeLanguages(tenancy.Languages{Default: "pt-PT", Others: []string{"en"}})
	router := mountAs(t, caller{}, func(d *admin.Deps) {
		d.Language = accountUsers{users}
		d.Messages = xtext.Load("en", admin.Catalogue())
	})
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/app/admin/language", strings.NewReader("locale=de"))
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsupported language returned %d, want 422: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, `lang="pt-PT"`) || strings.Contains(body, "is not a language this tenant is served in") {
		t.Errorf("Portuguese account refusal declares %s; English reason present=%t",
			regexp.MustCompile(`lang="[^"]*"`).FindString(body), strings.Contains(body, "is not a language this tenant is served in"))
	}
}
