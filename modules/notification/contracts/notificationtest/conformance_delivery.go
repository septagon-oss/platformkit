package notificationtest

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

func writeNotification(t *testing.T, f Fixture) {
	got, err := f.Service.Notify(f.Ctx, f.Tx, notice("  A task was assigned to you  "))
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	switch {
	case got.RecipientID != Ada:
		t.Errorf("the notice went to %s, want %s", got.RecipientID, Ada)
	case got.Title != "A task was assigned to you":
		t.Errorf("title is %q, want it trimmed", got.Title)
	case got.ReadAt != nil:
		t.Error("a new notification is already read")
	}
	published(t, f, contracts.EventCreated)
}

func validateNotice(t *testing.T, f Fixture) {
	for what, n := range map[string]contracts.Notice{
		"nobody":           {Title: "hello"},
		"nothing":          {Recipient: Ada},
		"an absolute link": {Recipient: Ada, Title: "hello", Link: "https://elsewhere.example.com"},
	} {
		if _, err := f.Service.Notify(f.Ctx, f.Tx, n); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("Notify with %s = %v, want ErrInvalid", what, err)
		}
	}
}

func requestMail(t *testing.T, f Fixture) {
	n := notice("A task was assigned to you")
	n.Wants = contracts.WantsInApp | contracts.WantsEmail
	if _, err := f.Service.Notify(f.Ctx, f.Tx, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	// Two events and no mail server: the send happens in the worker,
	// so a request never waits on somebody else's machine.
	published(t, f, contracts.EventCreated, contracts.EventEmailRequested)
}

func deliverFlaggedMail(t *testing.T, f Fixture) {
	n := notice("A task was assigned to you")
	n.Email = true
	if _, err := f.Service.Notify(f.Ctx, f.Tx, n); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	// The boolean is the spelling notices had before they named channels,
	// and applications written against that contract are still compiled.
	// It asks for the channel it stood for: an upgrade that read it as a
	// notice asking for nothing would stop the mail with no error.
	published(t, f, contracts.EventCreated, contracts.EventEmailRequested)
}

func retainNoticeWithoutAddress(t *testing.T, f Fixture) {
	n := notice("A task was assigned to you")
	n.Recipient = Bob
	n.Wants = contracts.WantsInApp | contracts.WantsEmail
	got, err := f.Service.Notify(f.Ctx, f.Tx, n)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if got.RecipientID != Bob {
		t.Errorf("the notice went to %s, want %s", got.RecipientID, Bob)
	}
	// Refusing the whole call would mean somebody with no address
	// could not be told anything.
	published(t, f, contracts.EventCreated)
}
