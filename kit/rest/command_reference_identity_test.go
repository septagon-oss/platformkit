package rest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestCommandReferencesResolveTheWholeResourceAddress(t *testing.T) {
	for _, system := range []bool{false, true} {
		name := "offered"
		if system {
			name = "system"
		}
		t.Run(name, func(t *testing.T) {
			api, _, _ := mounted(t)
			rest.Command(api.Surfaces(spec.Module), spec, "assign", "Assign", "Assign an owner", nil,
				func(context.Context, db.Tx[db.Tenant], uuid.UUID, referencedCommandOwner) (*Task, error) {
					t.Fatal("checking a reference executed the command")
					return nil, nil
				}, rest.CommandOptions{Present: entity.CommandHints{System: system}})

			// Neither a matching module nor a matching entity alone resolves a target.
			resources := append(api.Resources(),
				httpx.Resource{Module: "absent", Entity: "group"},
				httpx.Resource{Module: "other", Entity: "person"})
			bad := rest.CheckReferences(resources)
			for _, want := range []string{spec.Module + "/" + spec.Entity, "assign", "owner", "absent/person"} {
				if !strings.Contains(bad, want) {
					t.Fatalf("unresolved command reference must identify %q; refusal = %q", want, bad)
				}
			}

			// Composition order cannot decide whether a valid reference resolves.
			target := httpx.Resource{Module: "absent", Entity: "person"}
			for _, ordered := range [][]httpx.Resource{
				append(append([]httpx.Resource{}, resources...), target),
				append([]httpx.Resource{target}, resources...),
			} {
				if bad := rest.CheckReferences(ordered); bad != "" {
					t.Fatalf("registered command target was refused: %s", bad)
				}
			}
			if bad := rest.CheckReferences(resources); bad == "" {
				t.Fatal("a previous composition's target leaked into this composition")
			}
		})
	}
}
