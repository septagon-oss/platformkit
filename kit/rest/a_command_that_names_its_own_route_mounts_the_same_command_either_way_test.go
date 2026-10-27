package rest_test

// Two facts about a command decide which route exists — who may run it, and whether
// the path carries an id. A CommandSpec keeps them at its top level; the deprecated
// positional form is handed them inside CommandOptions and lifts them out on the way
// to the one body both forms reach. A lift that dropped either fact would leave the
// two spellings mounting different routes under the same words, so both are declared
// here, beside the hints that draw the button, and the two records are compared.

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestACommandNamingItsOwnGuardAndRouteMountsTheSameCommandEitherWay(t *testing.T) {
	// The two route facts live in the one field each form keeps them in: the
	// positional form has nothing but Options, and a CommandSpec keeps them beside
	// the verb and refuses a second copy in Options. What is compared is the command
	// both mount, not the field each author had to write.
	declare := func(surfaces httpx.Surfaces, positional bool) {
		run := func(context.Context, db.Tx[db.Tenant], uuid.UUID, publishBody) (rest.Response[string], error) {
			return rest.Response[string]{Result: "published"}, nil
		}
		present := entity.CommandHints{Label: "Publish now", Primary: true}
		if positional {
			rest.CommandResult(surfaces, spec, "publish", "Publish a note", "Makes the note visible to the tenant.",
				[]string{spec.Event(rest.Updated)}, run,
				rest.CommandOptions{Auth: httpx.SignedIn(), Collection: true, Present: present})
			return
		}
		rest.MountResult(surfaces, spec, rest.CommandSpec[publishBody]{
			Verb: "publish", Summary: "Publish a note", Doc: "Makes the note visible to the tenant.",
			Emits: []string{spec.Event(rest.Updated)}, Auth: httpx.SignedIn(), Collection: true,
			Options: rest.CommandOptions{Present: present},
		}, run)
	}
	shapes := map[string][2]any{}
	for _, positional := range []bool{true, false} {
		name := "the declaration"
		if positional {
			name = "the positional form"
		}
		api, _, _ := mountAs(t, spec, caller{}, func(api *httpx.API) { declare(api.Surfaces(spec.Module), positional) })
		op := recorded(t, api, "tasks-task-publish")
		if op == nil {
			t.Fatalf("%s mounted no route", name)
		}
		commands := api.Resources()[0].Commands
		if len(commands) != 1 {
			t.Fatalf("%s recorded %d commands, want the one", name, len(commands))
		}
		mounted := commands[0]
		mounted.Execute, mounted.Run = nil, nil
		shapes[name] = [2]any{*op, mounted}
	}
	for _, i := range []int{0, 1} {
		what := "command"
		if i == 0 {
			what = "route"
		}
		if !reflect.DeepEqual(shapes["the positional form"][i], shapes["the declaration"][i]) {
			t.Errorf("the two spellings are not the same %s:\n%#v\nagainst\n%#v",
				what, shapes["the positional form"][i], shapes["the declaration"][i])
		}
	}
	// And what both say is a command the author, not the Spec, decided: signed in,
	// at the collection route, drawn with the author's own word.
	command := shapes["the declaration"][1].(httpx.Command)
	if got := command.Auth.String(); got != "signed_in" {
		t.Errorf("the declared command is guarded by %s, want signed_in", got)
	}
	if !command.Collection {
		t.Error("the declared command lost its collection route")
	}
}
