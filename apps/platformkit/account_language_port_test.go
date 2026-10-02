package main

// The stub check apps/platformkit/README.md asks for when a file adds a runtime
// type here: accountLanguage is composition's own adapter, so the check asks what
// it does with it. It is the shell's account control seen from the other side of
// the port — the same user service the page language reads is the one that answers
// and receives the choice — and the answer has to be the command's, not a second
// opinion written next to the form.

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestTheAccountLanguagePortIsTheUserCommandSeenFromTheShell(t *testing.T) {
	users := usertest.NewFake()
	users.ServeLanguages(tenancy.Languages{Default: "en", Others: []string{"pt-PT"}})
	var tx db.Tx[db.Tenant]
	ctx := context.Background()

	ada, err := users.Invite(ctx, tx, "ada@acme.example.com", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	port := accountLanguage{users: users}
	if got, err := port.Preferred(ctx, tx, ada.ID); err != nil || got != "" {
		t.Fatalf("Preferred of a person who never chose = %q, %v", got, err)
	}

	// The write is the command, including the command's refusal: a tag this tenant
	// does not serve is not stored, whatever a stale form posts.
	ctx = tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: ada.ID})
	if err := port.Choose(ctx, tx, "pt-PT"); err != nil {
		t.Fatalf("Choose(pt-PT): %v", err)
	}
	if got, err := port.Preferred(ctx, tx, ada.ID); err != nil || got != "pt-PT" {
		t.Fatalf("Preferred after choosing = %q, %v; want pt-PT", got, err)
	}
	if err := port.Choose(ctx, tx, "de"); err == nil {
		t.Fatal("Choose(de), which this tenant does not serve, was accepted")
	}
	if got, _ := port.Preferred(ctx, tx, ada.ID); got != "pt-PT" {
		t.Fatalf("the refused choice changed the row to %q", got)
	}
	// Taking it back is the empty tag, and it is a value rather than an absence.
	if err := port.Choose(ctx, tx, ""); err != nil {
		t.Fatalf("Choose(empty): %v", err)
	}
	if got, _ := port.Preferred(ctx, tx, ada.ID); got != "" {
		t.Fatalf("Preferred after clearing = %q, want empty", got)
	}
}
