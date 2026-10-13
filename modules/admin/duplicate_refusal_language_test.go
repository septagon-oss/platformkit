package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestDuplicateUserRefusesInTheRequestsLanguage(t *testing.T) {
	owner, router := userLanguageRouter(t)
	created := postTaskInPortuguese(t, router, "/app/user/users", "email=reader%40language.test&displayName=Reader")
	if created.Code != http.StatusSeeOther || created.Header().Get("Location") == "" {
		t.Fatalf("create = %d, location %q", created.Code, created.Header().Get("Location"))
	}
	var beforeRow, afterRow string
	var beforeEvents, afterEvents int
	if err := owner.QueryRowContext(t.Context(), `SELECT COALESCE(json_agg(u ORDER BY u.id)::text, '[]'), (SELECT count(*) FROM platformkit_outbox) FROM users u`).Scan(&beforeRow, &beforeEvents); err != nil {
		t.Fatal(err)
	}
	refused := postTaskInPortuguese(t, router, "/app/user/users", "email=reader%40language.test&displayName=Another+reader")
	if (refused.Code != http.StatusUnprocessableEntity && refused.Code != http.StatusConflict) || !strings.HasPrefix(refused.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("duplicate refusal = %d (%s), want HTML 422 or 409", refused.Code, refused.Header().Get("Content-Type"))
	}
	body := refused.Body.String()
	if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) {
		t.Errorf("Portuguese duplicate refusal declares Content-Language=%q, pt-PT document=%t", refused.Header().Get("Content-Language"), strings.Contains(body, `lang="pt-PT"`))
	}
	if strings.Contains(body, "A record already uses one of these values") {
		t.Error("Portuguese duplicate refusal retains English reason: A record already uses one of these values. Change the duplicate value and try again.")
	}
	if err := owner.QueryRowContext(t.Context(), `SELECT COALESCE(json_agg(u ORDER BY u.id)::text, '[]'), (SELECT count(*) FROM platformkit_outbox) FROM users u`).Scan(&afterRow, &afterEvents); err != nil {
		t.Fatal(err)
	}
	if afterRow != beforeRow || afterEvents != beforeEvents {
		t.Errorf("duplicate refusal changed stored row=%t or event count=%d (before %d)", afterRow != beforeRow, afterEvents, beforeEvents)
	}
}
