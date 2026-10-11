package rest_test

import (
	"net/http"
	"strings"
	"testing"
)

// TestEveryPatchPropertyDocumentedNullableAcceptsNull holds the document's null
// promise against the one decoder: a key whose TaskPatch property admits null is
// cleared by a PATCH that sends null, and a key that admits no null is not
// advertised as nullable by accident of its JSON tag.
func TestEveryPatchPropertyDocumentedNullableAcceptsNull(t *testing.T) {
	s := spec
	s.Immutable = []string{"status"}
	docAPI, docRouter := patchDocumentAPI()
	s.Mount(docAPI.Surfaces(s.Module))
	patch := patchComponents(t, docRouter)["TaskPatch"]
	if patch == nil {
		t.Fatal("TaskPatch is missing from the served document")
	}
	nullable := map[string]bool{}
	for name, property := range patch.Properties {
		for _, arm := range property.AnyOf {
			if arm.Type == "null" {
				nullable[name] = true
			}
		}
	}
	for _, want := range []string{"dueAt", "tags"} {
		if !nullable[want] {
			t.Errorf("%s is a pointer or list but TaskPatch does not admit null", want)
		}
	}
	for _, scalar := range []string{"title", "priority", "notes", "done"} {
		if nullable[scalar] {
			t.Errorf("%s is a value field but TaskPatch admits null", scalar)
		}
	}

	_, router, _ := mount(t, s)
	code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"nullable","dueAt":"2030-01-01T00:00:00Z"}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	at := "/api/v1/tasks/task/" + id(t, body)
	for name := range nullable {
		if code, body := call(t, router, http.MethodPatch, at, `{"`+name+`":null}`); code != http.StatusOK {
			t.Errorf("PATCH %s:null = %d %s, the document says null is accepted", name, code, body)
		}
	}
	if code, body := call(t, router, http.MethodGet, at, ""); code != http.StatusOK || strings.Contains(body, `"dueAt"`) {
		t.Errorf("dueAt:null did not clear the deadline: %d %s", code, body)
	}
}
