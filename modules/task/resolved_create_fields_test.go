package task_test

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestResolvedCreatePolicyOffersOnlyFieldsTheCreateRouteAccepts(t *testing.T) {
	api, router := mounted(t)
	var entry screens.Entry
	for _, r := range api.Resources() {
		if r.Module == "task" && r.Entity == "task" {
			entry = screens.Describe1(r, true)
		}
	}
	if entry.Resolved == nil || !slices.Contains(entry.Resolved.Layout.Create, "title") {
		t.Fatal("the mounted task has no resolved create form with a title")
	}
	for _, name := range entry.Immutable {
		if slices.Contains(entry.Resolved.Layout.Create, name) {
			t.Errorf("resolved create form offers command-owned field %q", name)
		}
		for _, field := range entry.Resolved.Fields {
			if field.Name == name && field.Editable.Create {
				t.Errorf("resolved field %q advertises editable.create=true", name)
			}
		}
	}
	values := map[string]any{"title": "Create from the published form"}
	if slices.Contains(entry.Resolved.Layout.Create, "slaBreached") {
		values["slaBreached"] = false
	}
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	if code, reply := call(t, router, http.MethodPost, path, string(body)); code != http.StatusCreated {
		t.Errorf("POST using the resolved create controls = %d %s; want 201", code, reply)
	}
}
