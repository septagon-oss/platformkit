package rest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
)

type referencedCommandOwner struct {
	Owner string `json:"owner" ui:"reference:absent/person"`
}

func TestACommandReferenceNamesARegisteredResource(t *testing.T) {
	api, _, _ := mounted(t)
	rest.Command(api.Surfaces(spec.Module), spec, "assign", "Assign", "Assign an owner", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, referencedCommandOwner) (*Task, error) {
			return nil, nil
		}, rest.CommandOptions{})
	if bad := rest.CheckReferences(api.Resources()); !strings.Contains(bad, "absent/person") {
		t.Fatalf("command references must name an existing resource; refusal = %q", bad)
	}
}
