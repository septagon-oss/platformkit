package task_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestAPastDueDateWithoutAnSLADeadlineRecordsNoBreach: only slaDeadline
// breaches on arrival; dueAt alone never does.
func TestAPastDueDateWithoutAnSLADeadlineRecordsNoBreach(t *testing.T) {
	_, router := mounted(t)
	code, body := call(t, router, http.MethodPost, path, `{"title":"old","dueAt":"2020-01-01T00:00:00Z"}`)
	if code != http.StatusCreated || strings.Contains(body, `"slaBreached":true`) {
		t.Errorf("POST = %d %s, want 201 without slaBreached", code, body)
	}
}
