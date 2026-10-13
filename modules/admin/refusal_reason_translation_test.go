package admin_test

import (
	"html"
	"net/http"
	"strings"
	"testing"
)

func TestUserRefusalsRenderAPortugueseReasonAndPreserveRows(t *testing.T) {
	for _, tc := range []struct {
		name, suffix, form, reason string
		status                     int
	}{
		{"reserved_handle", "/handle", "handle=admin", `o nome curto "admin" pertence à plataforma ou a uma função, não a uma pessoa`, http.StatusUnprocessableEntity},
		{"duplicate_email", "", "email=reader%40language.test&displayName=Another+reader", "Um registo já usa um destes valores. Altere o valor duplicado e tente novamente.", http.StatusUnprocessableEntity},
		{"invalid_email", "", "email=invalid&displayName=Reader", `"invalid" não é um endereço de email`, http.StatusUnprocessableEntity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, router := userLanguageRouter(t)
			created := postTaskInPortuguese(t, router, "/app/user/users", "email=reader%40language.test&displayName=Reader")
			if created.Code != http.StatusSeeOther || created.Header().Get("Location") == "" {
				t.Fatalf("create = %d, location %q", created.Code, created.Header().Get("Location"))
			}
			snapshot := func() (string, int) {
				t.Helper()
				var rows string
				var events int
				if err := owner.QueryRowContext(t.Context(), `SELECT COALESCE(json_agg(u ORDER BY u.id)::text, '[]'), (SELECT count(*) FROM platformkit_outbox) FROM users u`).Scan(&rows, &events); err != nil {
					t.Fatal(err)
				}
				return rows, events
			}
			before, eventsBefore := snapshot()
			path := "/app/user/users"
			if tc.suffix != "" {
				path = created.Header().Get("Location") + tc.suffix
			}
			refused := postTaskInPortuguese(t, router, path, tc.form)
			if refused.Code != tc.status {
				t.Fatalf("refusal = %d, want %d: %s", refused.Code, tc.status, refused.Body.String())
			}
			body := html.UnescapeString(refused.Body.String())
			if !strings.Contains(body, tc.reason) {
				t.Errorf("refusal must explain the rule in Portuguese, want %q: %s", tc.reason, body)
			}
			if refused.Header().Get("Content-Language") != "pt-PT" || !strings.Contains(body, `lang="pt-PT"`) || !strings.Contains(body, `dir="ltr"`) {
				t.Error("refusal must declare Portuguese and its direction")
			}
			after, eventsAfter := snapshot()
			if after != before || eventsAfter != eventsBefore {
				t.Errorf("refusal changed rows=%t or outbox=%d (before %d)", after != before, eventsAfter, eventsBefore)
			}
		})
	}
}
