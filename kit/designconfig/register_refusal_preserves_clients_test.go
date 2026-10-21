package designconfig_test

import (
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/designconfig"
)

func TestARefusedIdentityLeavesBothRegisteredClientsUnchanged(t *testing.T) {
	t.Parallel()
	register := designconfig.NewRegister()
	for _, slug := range []string{"alpha", "beta"} {
		client, err := designconfig.LoadClientDesign(os.DirFS("testdata/set"), slug)
		if err != nil {
			t.Fatal(err)
		}
		if err := register.Add(client); err != nil {
			t.Fatal(err)
		}
		if pair, ok := register.Pair(slug); !ok || pair == (design.Pair{}) {
			t.Fatalf("accepted client %s has no identity", slug)
		}
	}
	before := maps.Clone(register.Pairs())
	client, err := designconfig.LoadClientDesign(os.DirFS("testdata/clients"), "broken")
	if err != nil {
		t.Fatal(err)
	}
	if err := register.Add(client); err == nil || !strings.Contains(err.Error(), "text-muted") {
		t.Fatalf("unreadable identity must be refused with its token: %v", err)
	}
	if !maps.Equal(before, register.Pairs()) {
		t.Fatal("refusing a third identity changed a registered client")
	}
	if pair, ok := register.Pair("broken"); ok || pair != (design.Pair{}) {
		t.Fatal("a refused identity returned a registered pair")
	}

	// A caller may edit its snapshot without changing either client's identity.
	snapshot := register.Pairs()
	delete(snapshot, "alpha")
	snapshot["beta"] = design.Pair{}
	if !maps.Equal(before, register.Pairs()) {
		t.Fatal("editing a returned snapshot changed a registered client")
	}
}
