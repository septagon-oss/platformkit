package module

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// handler is the callback a subscription needs in order to be a subscription.
func handler(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }

// TestASubscriptionNamingNoModuleIsTheManifestsOwn is the fill: a manifest that
// leaves Module empty means its own module, which is the answer Validate already
// gives — and the durable consumer name has to come out identical to the one the
// explicit spelling gives, because a filled name that landed on a different
// consumer would redeliver every event this installation already handled.
func TestASubscriptionNamingNoModuleIsTheManifestsOwn(t *testing.T) {
	emitted := []string{"task.task.created"}
	stated := Module{Name: "task", Events: emitted,
		Subscriptions: []events.Subscription{{Module: "task", Name: "task.task.created", Handler: handler}}}
	leftBlank := Module{Name: "task", Events: emitted,
		Subscriptions: []events.Subscription{{Name: "task.task.created", Handler: handler}}}

	filled := Expand([]Module{leftBlank})[0]
	if got := filled.Subscriptions[0].Module; got != "task" {
		t.Fatalf("the expansion attributed the subscription to %q, want the manifest's own name", got)
	}
	if err := Validate(Expand([]Module{leftBlank})); err != nil {
		t.Fatalf("a subscription the kernel attributed is refused: %v", err)
	}
	app := appname.MustParse("acme")
	if want, got := appname.Durable(app, "task", "task.task.created"), appname.Durable(app, filled.Subscriptions[0].Module, filled.Subscriptions[0].Name); want != got {
		t.Errorf("the filled subscription's durable is %q and the explicit one's is %q", got, want)
	}
	// The input is not written: a manifest is a package-level value, and one
	// composition filling it in would be a second composition reading a mutated
	// copy of the same module.
	if stated.Subscriptions[0].Module != "task" || leftBlank.Subscriptions[0].Module != "" {
		t.Error("Expand wrote the manifest it was given")
	}
}

func TestASubscriptionAttributedToAnotherModuleIsStillRefused(t *testing.T) {
	m := Module{Name: "task", Events: []string{"task.task.created"},
		Subscriptions: []events.Subscription{{Module: "billing", Name: "task.task.created", Handler: handler}}}
	err := Validate(Expand([]Module{m}))
	if err == nil || !strings.Contains(err.Error(), "billing") {
		t.Fatalf("a subscription naming another module was not refused naming it: %v", err)
	}
}

// TestANavEntryNamingOnlyItsEntityNamesItsOwnScreen is the other fill: "tasks"
// inside module task is the task module's screen. An entry that already names a
// module keeps what it wrote — including another module's name, which is a link
// to a screen somebody else serves and the only reason the field is two segments.
func TestANavEntryNamingOnlyItsEntityNamesItsOwnScreen(t *testing.T) {
	m := Module{
		Name:        "task",
		Permissions: []Permission{{Key: "task:read", Label: "read tasks"}},
		Nav: []NavEntry{
			{Label: "Own", Screen: "tasks", Permission: "task:read"},
			{Label: "Spelled", Screen: "task/tasks", Permission: "task:read"},
			{Label: "Elsewhere", Screen: "billing/plans", Permission: "task:read"},
		},
	}
	filled := Expand([]Module{m})
	out := filled[0]
	want := []string{"task/tasks", "task/tasks", "billing/plans"}
	for i, entry := range out.Nav {
		if entry.Screen != want[i] {
			t.Errorf("entry %q resolves to %q, want %q", entry.Label, entry.Screen, want[i])
		}
	}
	if m.Nav[0].Screen != "tasks" {
		t.Error("Expand wrote the manifest it was given")
	}
	if err := Validate(filled); err != nil {
		t.Fatalf("the filled navigation is refused: %v", err)
	}
}

func TestANavEntryNamingNoScreenAtAllIsStillRefused(t *testing.T) {
	m := Module{Name: "task", Permissions: []Permission{{Key: "task:read", Label: "read tasks"}},
		Nav: []NavEntry{{Label: "Nothing", Permission: "task:read"}}}
	err := Validate(Expand([]Module{m}))
	if err == nil || !strings.Contains(err.Error(), "Nothing") {
		t.Fatalf("an entry with no screen was not refused naming it: %v", err)
	}
}
