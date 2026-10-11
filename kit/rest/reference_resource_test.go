package rest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

type referencedOwner struct {
	crud.Base
	Owner string `json:"owner" ui:"reference:user/user"`
}

func (*referencedOwner) TableName() string { return "rest_referenced_owners" }

func TestAMountedReferenceResolvesTheRegisteredResource(t *testing.T) {
	api, _, _ := mountAs(t, rest.Spec[*referencedOwner]{
		Module: "note", Entity: "note", Path: "/notes", Read: "note:read", Write: "note:write",
	}, caller{})
	resources := append(api.Resources(), httpx.Resource{Module: "user", Entity: "user"})
	if bad := rest.CheckReferences(resources); bad != "" {
		t.Fatalf("a mounted module/entity reference to a registered resource was refused: %s", bad)
	}
}
