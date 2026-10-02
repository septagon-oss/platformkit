package rest_test

// write_address_test.go reads the projection rather than the refusal. A
// resource's write address is quoted twice over: ui/screens prints it as the
// catalogue's `write_path` and kit/httpx quotes it at a caller who posted to the
// read door of a resource whose writes stand elsewhere. Both read the one field,
// so both are wrong or both are right together.
//
// Which address that is has never been a question about which *router* the Spec
// was mounted on — the operator's surface or the workspace's — only about
// whether a route was mounted at all, and that is `Operations`' answer.

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestTheWriteAddressAResourceNamesIsTheDoorItMounted reads the projection rather
// than the refusal. `WritePath` is what the catalogue prints as `write_path` and
// what the kernel's own refusal quotes as where to write; both read this one
// field. A Spec whose writes belong to the installation and whose operation set
// names no write mounts no write on either surface, so it names no address at
// all; the same Spec with create offered names the control-plane door, because
// that is the one that takes the POST.
func TestTheWriteAddressAResourceNamesIsTheDoorItMounted(t *testing.T) {
	f := &operatorReader{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "ops", Operator: true}, allow: true}
	for _, tc := range []struct {
		name   string
		verbs  []httpx.CRUD
		writes bool
	}{
		{"a write offered", []httpx.CRUD{rest.List, rest.Read, rest.Create}, true},
		{"no write offered", []httpx.CRUD{rest.List, rest.Read}, false},
	} {
		s := spec
		s.Operations = tc.verbs
		s.OperatorWrite = true
		api, _, _ := mountAs(t, s, f)
		want := ""
		if tc.writes {
			want = api.Surfaces(s.Module).Ops.Prefix() + s.Path
		}
		if got := api.Resources()[0].WritePath; got != want {
			t.Errorf("%s: the resource names %q as its write address, want %q", tc.name, got, want)
		}
	}
}
