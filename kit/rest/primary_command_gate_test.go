package rest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestAPrimaryCommandARowCannotSwipeIsRefusedNamingIt mounts one resource per
// case, its commands registered the way a module registers them, and asks the
// boot gate over the API's own resource list: a verb that is not mounted, one
// that takes arguments, a system verb and a collection verb are each refused with
// the hint and the verb named; a mounted, argument-free row verb is served.
func TestAPrimaryCommandARowCannotSwipeIsRefusedNamingIt(t *testing.T) {
	type reason struct {
		Why string `json:"why"`
	}
	run := func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil }
	withReason := func(context.Context, db.Tx[db.Tenant], uuid.UUID, reason) (*Task, error) { return nil, nil }
	for _, tc := range []struct {
		name    string
		primary string
		want    []string
	}{
		{"a mounted row verb with no arguments", "archive", nil},
		{"a verb nobody mounted", "vanish", []string{"primaryCommand", `"vanish"`, "not a command"}},
		{"a verb that takes arguments", "explain", []string{"primaryCommand", `"explain"`, "takes arguments"}},
		{"a system verb", "purge", []string{"primaryCommand", `"purge"`, "system command"}},
		{"a collection verb", "sweep", []string{"primaryCommand", `"sweep"`, "collection"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := spec
			s.Present = entity.EntryHints{PrimaryCommand: tc.primary}
			api, _ := patchDocumentAPI()
			s.Mount(api.Surfaces(s.Module))
			surfaces := api.Surfaces(s.Module)
			rest.Command(surfaces, s, "archive", "Archive", "Archive the task.", nil, run, rest.CommandOptions{})
			rest.Command(surfaces, s, "explain", "Explain", "Explain the task.", nil, withReason, rest.CommandOptions{})
			rest.Command(surfaces, s, "purge", "Purge", "An operator's door.", nil, run,
				rest.CommandOptions{Present: entity.CommandHints{System: true}})
			rest.Command(surfaces, s, "sweep", "Sweep", "Sweep every task.", nil, run,
				rest.CommandOptions{Collection: true})

			bad := rest.CheckRowHints(api.Resources())
			if tc.want == nil {
				if bad != "" {
					t.Fatalf("a row verb a swipe can run is refused: %s", bad)
				}
				return
			}
			if bad == "" {
				t.Fatalf("primaryCommand %q is served", tc.primary)
			}
			for _, w := range tc.want {
				if !strings.Contains(bad, w) {
					t.Errorf("the refusal %q does not name %s", bad, w)
				}
			}
		})
	}
}
