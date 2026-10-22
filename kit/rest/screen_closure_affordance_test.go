package rest_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// A verb the resource offers but whose closure is absent has no screen, so the
// pages a writer is shown link to none: the record page of an update-less,
// delete-less resource offers no Edit link and no delete form, while the list
// still offers the New form that is mounted. An absent New form is a 404 page.
// The record address keeps its UUID contract: a malformed id is refused before
// any row is read.
func TestARecordPageLinksOnlyToTheWriteScreensThatExist(t *testing.T) {
	for _, absent := range []string{"update and delete", "create"} {
		t.Run(absent, func(t *testing.T) {
			api, router, admin := mountAs(t, spec, member{"task:read": true, "task:write": true})
			row := seed(t, admin, "A row with fewer doors")
			r := api.Resources()[0]
			reads := 0
			get := r.Get
			r.Get = func(ctx context.Context, id uuid.UUID) (map[string]any, error) {
				reads++
				return get(ctx, id)
			}
			if absent == "create" {
				r.Create = nil
			} else {
				r.Update, r.Delete = nil, nil
			}
			screens.Mount(api.Surfaces(r.Module).App, page.Shell{Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
				return g.Group(body)
			}}, screens.Options{}, r)

			code, list := call(t, router, http.MethodGet, "/app/tasks/task", "")
			if code != http.StatusOK {
				t.Fatalf("list = %d %s", code, list)
			}
			if want := absent != "create"; strings.Contains(list, "/app/tasks/task/new") != want {
				t.Errorf("list offers New = %v, want %v: %s", !want, want, list)
			}

			code, record := call(t, router, http.MethodGet, "/app/tasks/task/"+row, "")
			if code != http.StatusOK || !strings.Contains(record, "A row with fewer doors") {
				t.Fatalf("record = %d %s", code, record)
			}
			edit, remove := strings.Contains(record, row+"/edit"), strings.Contains(record, row+"/delete")
			if want := absent == "create"; edit != want || remove != want {
				t.Errorf("record offers edit=%v delete=%v, want both %v: %s", edit, remove, want, record)
			}
			if absent != "create" {
				if code, body := call(t, router, http.MethodGet, "/app/tasks/task/"+row+"/edit", ""); code != http.StatusNotFound {
					t.Errorf("GET absent edit = %d, want 404: %s", code, body)
				}
			} else {
				// The reserved word is answered as a page, the shell's own refusal,
				// never as the problem document the JSON routes write.
				code, body, head := callHead(t, router, http.MethodGet, "/app/tasks/task/new")
				if code != http.StatusNotFound || !strings.HasPrefix(head.Get("Content-Type"), "text/html") ||
					!strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), "<!doctype html") {
					t.Errorf("GET absent new = %d %q, want a 404 page: %.120s", code, head.Get("Content-Type"), body)
				}
			}

			before := reads
			if code, body := call(t, router, http.MethodGet, "/app/tasks/task/not-a-record", ""); code != http.StatusUnprocessableEntity {
				t.Errorf("GET malformed record id = %d, want 422: %s", code, body)
			}
			if reads != before {
				t.Errorf("a malformed record id read %d rows", reads-before)
			}
		})
	}
}
