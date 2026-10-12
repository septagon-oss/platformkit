package admin_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestGeneratedFormDecodeRefusalsSpeakTheRequestsLanguage(t *testing.T) {
	for _, tc := range []struct {
		name, form, english string
	}{
		{"malformed form", "title=%ZZ", "this form could not be read"},
		{"invalid timestamp", "title=Language+case&dueAt=tomorrow", "is not a time"},
		{"command owned field", "title=Language+case&resolution=Finished", "belongs to a route of its own, not to this form"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner, router := taskLanguageRouter(t)
			w := postTaskInPortuguese(t, router, "/app/task/tasks", tc.form)
			if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), `name="title"`) {
				t.Fatalf("invalid task form = %d, want a 422 with its title control", w.Code)
			}
			if !strings.Contains(w.Body.String(), `lang="pt-PT"`) {
				t.Error("Portuguese form refusal does not declare pt-PT")
			}
			if strings.Contains(w.Body.String(), tc.english) {
				t.Errorf("Portuguese form refusal retains English reason %q", tc.english)
			}
			var rows, events int
			if err := owner.QueryRowContext(t.Context(),
				`SELECT (SELECT count(*) FROM tasks), (SELECT count(*) FROM platformkit_outbox)`).Scan(&rows, &events); err != nil {
				t.Fatal(err)
			}
			if rows != 0 || events != 0 {
				t.Errorf("refused creation left %d tasks and %d events", rows, events)
			}
		})
	}
}
