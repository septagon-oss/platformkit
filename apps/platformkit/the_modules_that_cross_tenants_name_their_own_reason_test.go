package main

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
)

// systemAccessOf reads the cross-tenant declarations off the manifests this
// composition carries. A harness that builds kit/httpx by hand — instead of
// through kit/app, which fills the same value from the same manifests — has to
// state them, and reading them here keeps the sentences from drifting away from
// the modules that wrote them.
func systemAccessOf(modules []module.Module) map[string]string {
	out := map[string]string{}
	for _, m := range modules {
		if m.SystemAccess.Declared() {
			out[m.Name] = m.SystemAccess.Reason
		}
	}
	return out
}

// TestTheModulesThatCrossTenantsNameFourDifferentReasons is the reason the
// declarations exist rather than the one shared token: a cross-tenant
// transaction's trail has to say which module opened it and for what. Four
// modules do this work in the reference application, each for a reason that
// describes its own job, and no fifth may start crossing tenants without
// saying so here — a composition refuses a token taken without a declaration
// and a declaration nothing took.
func TestTheModulesThatCrossTenantsNameFourDifferentReasons(t *testing.T) {
	want := map[string]string{
		"tenant":  "create, list and suspend tenants, which belong to no tenant",
		"admin":   "list every tenant for the workspace's tenant switcher",
		"billing": "read every tenant's subscription before a plan is removed",
		"file":    "sweep stored bytes no row references, across every tenant",
	}
	_, cfg := configure(t)
	seen := map[string]string{}
	for _, m := range compose(cfg).modules {
		if m.SystemAccess.Declared() {
			seen[m.Name] = m.SystemAccess.Reason
		}
	}
	for name, reason := range want {
		if seen[name] != reason {
			t.Errorf("module %q declares %q, want the reason its own comment already states: %q", name, seen[name], reason)
		}
		delete(seen, name)
	}
	for name, reason := range seen {
		t.Errorf("module %q declares cross-tenant work (%q) that no case names; state what it crosses tenants for, or stop crossing", name, reason)
	}
	// One sentence per module: four tokens whose reasons were one sentence would
	// be the shared token with four names on it. The map above compares each
	// module with its own expected sentence, which no shared reason passes.
	if strings.TrimSpace(want["file"]) == want["tenant"] {
		t.Fatal("the expected reasons are one sentence")
	}
}
