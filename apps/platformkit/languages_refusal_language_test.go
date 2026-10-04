package main

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// A stale language form keeps the page in the tenant's language, including the
// reason displayed above the form.
func TestALanguageRevisionRefusalSpeaksTheTenantsLanguage(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"pt-PT","supported":["en"]}`)
	acme := installationTenantID(t, cfg, admin, "acme")
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var oldRevision int64
	if err := owner.QueryRowContext(t.Context(),
		`SELECT locale_revision FROM tenants WHERE id = $1`, acme).Scan(&oldRevision); err != nil {
		t.Fatal(err)
	}
	declareLocale(t, cfg, admin, acme, `{"default":"pt-PT","supported":[]}`)
	status, body := postLanguages(t, cfg, admin, fmt.Sprint(oldRevision))
	if status != http.StatusOK || pageLang(body) != "pt-PT" {
		t.Fatalf("the stale form did not return a Portuguese page: status=%d lang=%q", status, pageLang(body))
	}
	alert := regexp.MustCompile(`(?s)<p[^>]*role="alert"[^>]*>(.*?)</p>`).FindStringSubmatch(body)
	if len(alert) != 2 {
		t.Fatalf("the refused form has no visible alert")
	}
	if !strings.Contains(alert[1], "revisão") {
		t.Errorf("the pt-PT refusal says %q, want a Portuguese reason naming the revision", alert[1])
	}
}
