package task_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestTheCreateRefusesTheFieldsTheLifecycleOwns: a POST that names a field a
// command owns is refused by name and stores nothing, as the PATCH is.
func TestTheCreateRefusesTheFieldsTheLifecycleOwns(t *testing.T) {
	_, router := mounted(t)
	for _, tt := range []struct{ field, body string }{
		{"assigneeId", `{"title":"a","assigneeId":"` + uuid.NewString() + `"}`},
		{"slaBreached", `{"title":"b","slaBreached":true,"slaDeadline":"2030-01-01T00:00:00Z"}`},
		{"resolution", `{"title":"c","resolution":"done"}`},
	} {
		code, body := call(t, router, http.MethodPost, path, tt.body)
		if code != http.StatusUnprocessableEntity || !strings.Contains(body, tt.field) {
			t.Errorf("POST %s = %d %s, want a 422 naming %s", tt.body, code, body, tt.field)
		}
	}
	if code, body := call(t, router, http.MethodGet, path, ""); code != http.StatusOK || strings.Contains(body, `"title":"`) {
		t.Errorf("GET = %d %s, want an empty list: every create was refused", code, body)
	}
}

// TestACreateWithAPastDeadlineAnswersTheBreach: the 201 shows the breached row.
func TestACreateWithAPastDeadlineAnswersTheBreach(t *testing.T) {
	_, router := mounted(t)
	code, body := call(t, router, http.MethodPost, path, `{"title":"late","slaDeadline":"2020-01-01T00:00:00Z"}`)
	if code != http.StatusCreated || !strings.Contains(body, `"slaBreached":true`) {
		t.Errorf("POST = %d %s, want 201 with slaBreached", code, body)
	}
}
