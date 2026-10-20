package notification_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// recipientField is the payload member a notice names its addressee with, read off
// the payload type rather than remembered: the policy, the frame and the connection
// all point at the same member, and a case that spelled it out could pass while the
// payload renamed it.
func recipientField() string {
	field, ok := reflect.TypeFor[contracts.Created]().FieldByName("Recipient")
	if !ok {
		panic("the payload carries no addressee for a recipient policy to read")
	}
	tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	return tag
}

// The two things that happen to one person's notices are watchable, and nobody but
// that person may watch them. The declaration is the whole of the answer — the
// endpoint asks it before a byte goes out — so it is asserted here, on the manifest
// that ships, rather than on a copy a case assembled.
func TestTheModuleOpensItsTwoNoticeEventsToTheirRecipientOnly(t *testing.T) {
	t.Parallel()
	_, manifest := notification.New(notification.Deps{Mailer: notification.NewMailbox(), Page: &notification.Page{}})
	want := []string{contracts.EventCreated, contracts.EventRead}
	if len(manifest.Streams) != len(want) {
		t.Fatalf("the module streams %d events, not the %d a person's own list changes on: %v",
			len(manifest.Streams), len(want), manifest.Streams)
	}
	for _, stream := range manifest.Streams {
		if !slices.Contains(want, stream.Event) {
			t.Errorf("the module streams %q, which is not one of the events a person's own list is drawn from", stream.Event)
		}
		if kind := stream.Policy.Kind(); kind != module.KindRecipient {
			t.Errorf("%s is watched under the %q policy: a notice is addressed to somebody, and a connection that tells everybody is a leak of who was told what",
				stream.Event, kind)
		}
		if field := stream.Policy.Field(); field != recipientField() {
			t.Errorf("%s reads the recipient from %q, which is not the member the payload spells (%q): the payload member is who was told, and it is the same user id the connection is opened as",
				stream.Event, field, recipientField())
		}
	}
}

// A stream is a door onto an event, and an event nobody emits has nothing behind
// the door; a policy that names nobody, or a recipient field outside the grammar a
// payload member has, is the same mistake in another shape. The kernel refuses all
// three before anything serves, which is what makes the declaration above a claim
// rather than a hope — and the recipient member itself is read at delivery, because
// the payload's shape lives as a Go type and not here.
func TestTheDeclarationPassesTheKernelsOwnValidation(t *testing.T) {
	t.Parallel()
	_, manifest := notification.New(notification.Deps{Mailer: notification.NewMailbox(), Page: &notification.Page{}})
	if err := module.Validate([]module.Module{manifest}); err != nil {
		t.Errorf("the shipped manifest is refused by the kernel: %v", err)
	}
	for _, bad := range []module.Module{
		{Name: "notification", Declared: contracts.Events,
			Streams: []module.Stream{{Event: "notification.never_happened", Policy: module.StreamRecipient("recipientId")}}},
		// The member a recipient policy reads is named as the payload spells it; a
		// name outside that grammar could match no payload at all.
		{Name: "notification", Declared: contracts.Events,
			Streams: []module.Stream{{Event: contracts.EventCreated, Policy: module.StreamRecipient("whom it was sent to")}}},
		{Name: "notification", Declared: contracts.Events,
			Streams: []module.Stream{{Event: contracts.EventCreated}}},
	} {
		err := module.Validate([]module.Module{bad})
		if err == nil {
			t.Errorf("the kernel accepted a stream of %v it must refuse", bad.Streams)
			continue
		}
		if !strings.Contains(err.Error(), "notification") {
			t.Errorf("the refusal does not name the module that wrote it: %v", err)
		}
	}
}
