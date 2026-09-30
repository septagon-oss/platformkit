package notificationtest_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// The channel decision, as data. These are the cases the module runs: contracts.Decide
// is the function the service, the fake and any provider-facing code share, so a
// case here is a case against all three, and a case that passes here and fails
// against Postgres would be a decision the two implementations did not share.
//
// Each case names who could correct the suppression it expects, because "why is
// nothing arriving" has a different answer for "this deployment sends no push"
// and for "she turned mail off", and the ledger has to carry that difference.
func TestDecideChannels(t *testing.T) {
	lisbon := &contracts.QuietHours{RecipientID: uuid.New(), StartMinute: 22 * 60, EndMinute: 7 * 60, TimeZone: "Europe/Lisbon"}
	verified := &contracts.Sender{
		Domain: "acme.example.com", Selector: "selector1", FromName: "Acme",
		FromAddress: "notifications@acme.example.com", Status: contracts.SenderVerified, Key: []byte("k"),
	}
	unverified := &contracts.Sender{
		Domain: "acme.example.com", Selector: "selector1", FromName: "Acme",
		FromAddress: "notifications@acme.example.com", Status: contracts.SenderPending, Key: []byte("k"),
	}
	unsigned := &contracts.Sender{
		Domain: "acme.example.com", Selector: "selector1", FromName: "Acme",
		FromAddress: "notifications@acme.example.com", Status: contracts.SenderVerified,
	}
	all := contracts.Channels
	mailOnly := []contracts.Channel{contracts.ChannelInApp, contracts.ChannelEmail}
	wantsAll := contracts.WantsAll

	cases := []struct {
		what       string
		wants      contracts.Wants
		class      string
		intent     string
		prefs      []contracts.Preference
		quiet      *contracts.QuietHours
		sender     *contracts.Sender
		scope      contracts.Scope
		chosen     []contracts.Channel
		suppressed []contracts.Suppression
	}{
		{
			what:   "a notice that asks for nothing says so in the application only",
			wants:  0,
			scope:  contracts.Scope{Now: noon, Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp},
		},
		{
			what:   "a deployment that sends only mail accounts for the rest",
			wants:  wantsAll,
			sender: verified,
			scope:  contracts.Scope{Now: noon, Available: mailOnly},
			chosen: []contracts.Channel{contracts.ChannelInApp, contracts.ChannelEmail},
			suppressed: []contracts.Suppression{
				{Channel: contracts.ChannelPush, Reason: "push is not sent by this deployment", Corrects: contracts.CorrectionDeployment},
				{Channel: contracts.ChannelWebPush, Reason: "web_push is not sent by this deployment", Corrects: contracts.CorrectionDeployment},
				{Channel: contracts.ChannelWebhook, Reason: "webhook is not sent by this deployment", Corrects: contracts.CorrectionDeployment},
			},
		},
		{
			what:   "a tenant with no sender sends no mail",
			wants:  contracts.WantsEmail,
			scope:  contracts.Scope{Now: noon, Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp},
			suppressed: []contracts.Suppression{{
				Channel: contracts.ChannelEmail, Corrects: contracts.CorrectionTenant,
				Reason: "this tenant has no sender: mail is refused until its administrator sets one",
			}},
		},
		{
			what:   "a sender nobody has verified sends no mail",
			wants:  contracts.WantsEmail,
			sender: unverified,
			scope:  contracts.Scope{Now: noon, Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp},
			suppressed: []contracts.Suppression{{
				Channel: contracts.ChannelEmail, Corrects: contracts.CorrectionTenant,
				Reason: "this tenant's sender notifications@acme.example.com has no verified DKIM record for selector1._domainkey.acme.example.com (status pending)",
			}},
		},
		{
			what:   "a verified sender this deployment holds no key for sends no mail",
			wants:  contracts.WantsEmail,
			sender: unsigned,
			scope:  contracts.Scope{Now: noon, Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp},
			suppressed: []contracts.Suppression{{
				Channel: contracts.ChannelEmail, Corrects: contracts.CorrectionDeployment,
				Reason: "this deployment holds no DKIM key to sign as selector1._domainkey.acme.example.com",
			}},
		},
		{
			what:   "a tenant that switched a channel off does not use it",
			wants:  wantsAll,
			sender: verified,
			scope: contracts.Scope{Now: noon, Available: all,
				Disabled: []contracts.Channel{contracts.ChannelWebPush}},
			chosen: []contracts.Channel{contracts.ChannelInApp, contracts.ChannelEmail, contracts.ChannelPush, contracts.ChannelWebhook},
			suppressed: []contracts.Suppression{{
				Channel: contracts.ChannelWebPush, Reason: "this tenant switched web_push off", Corrects: contracts.CorrectionTenant,
			}},
		},
		{
			what:   "the person's own answer about one intent",
			wants:  wantsAll,
			intent: "task.assigned",
			prefs: []contracts.Preference{
				{Intent: "", Channel: contracts.ChannelEmail, Enabled: false},
				{Intent: "task.assigned", Channel: contracts.ChannelEmail, Enabled: true},
			},
			sender: verified,
			scope:  contracts.Scope{Now: noon, Available: all},
			chosen: all,
		},
		{
			what:   "the blanket answer applies to every other intent",
			wants:  wantsAll,
			intent: "billing.invoice",
			prefs: []contracts.Preference{
				{Intent: "", Channel: contracts.ChannelEmail, Enabled: false},
				{Intent: "task.assigned", Channel: contracts.ChannelEmail, Enabled: true},
			},
			sender: verified,
			scope:  contracts.Scope{Now: noon, Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp, contracts.ChannelPush, contracts.ChannelWebPush, contracts.ChannelWebhook},
			suppressed: []contracts.Suppression{{
				Channel: contracts.ChannelEmail, Reason: "email is off for billing.invoice", Corrects: contracts.CorrectionRecipient,
			}},
		},
		{
			what:   "a quiet window holds the noise and not the alarm",
			wants:  wantsAll,
			intent: "task.assigned",
			quiet:  lisbon,
			sender: verified,
			// 21:00 UTC on 12 September is 22:00 in Lisbon (WEST), inside the window that
			// began at 22:00 and ends at 07:00.
			scope:  contracts.Scope{Now: time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC), Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp},
			suppressed: []contracts.Suppression{
				{Channel: contracts.ChannelEmail, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
				{Channel: contracts.ChannelPush, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
				{Channel: contracts.ChannelWebPush, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
				{Channel: contracts.ChannelWebhook, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
			},
		},
		{
			what:   "outside the window everything goes",
			wants:  wantsAll,
			intent: "task.assigned",
			quiet:  lisbon,
			sender: verified,
			scope:  contracts.Scope{Now: time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC), Available: all},
			chosen: all,
		},
		{
			what:   "a security notice crosses a quiet window and an opt-out",
			wants:  wantsAll,
			class:  contracts.ClassSecurity,
			intent: "auth.password_changed",
			prefs:  []contracts.Preference{{Intent: "", Channel: contracts.ChannelEmail, Enabled: false}},
			quiet:  lisbon,
			sender: verified,
			scope:  contracts.Scope{Now: time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC), Available: all},
			chosen: all,
		},
		{
			what:   "a security notice still cannot be sent from an unverified tenant",
			wants:  contracts.WantsEmail,
			class:  contracts.ClassSecurity,
			intent: "auth.password_changed",
			sender: unverified,
			scope:  contracts.Scope{Now: noon, Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp},
			suppressed: []contracts.Suppression{{
				Channel: contracts.ChannelEmail, Corrects: contracts.CorrectionTenant,
				Reason: "this tenant's sender notifications@acme.example.com has no verified DKIM record for selector1._domainkey.acme.example.com (status pending)",
			}},
		},
		{
			what:   "in-app is never suppressed",
			wants:  contracts.WantsAll,
			intent: "task.assigned",
			prefs: []contracts.Preference{
				{Intent: "", Channel: contracts.ChannelInApp, Enabled: false},
			},
			quiet:  lisbon,
			sender: verified,
			scope:  contracts.Scope{Now: time.Date(2026, 9, 12, 21, 0, 0, 0, time.UTC), Available: all},
			chosen: []contracts.Channel{contracts.ChannelInApp},
			suppressed: []contracts.Suppression{
				{Channel: contracts.ChannelEmail, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
				{Channel: contracts.ChannelPush, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
				{Channel: contracts.ChannelWebPush, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
				{Channel: contracts.ChannelWebhook, Reason: "quiet hours until 07:00 Europe/Lisbon", Corrects: contracts.CorrectionRecipient},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			d := contracts.Decide(c.wants, c.class, c.intent, c.prefs, c.quiet, c.sender, c.scope)
			if !slices.Equal(d.Chosen, c.chosen) {
				t.Errorf("chose %v, want %v", d.Chosen, c.chosen)
			}
			if len(d.Suppressed) != len(c.suppressed) {
				t.Fatalf("suppressed %d channels (%v), want %d", len(d.Suppressed), d.Suppressed, len(c.suppressed))
			}
			for i, want := range c.suppressed {
				got := d.Suppressed[i]
				if got.Channel != want.Channel || got.Reason != want.Reason || got.Corrects != want.Corrects {
					t.Errorf("suppression %d is %+v, want %+v", i, got, want)
				}
			}
			if n := len(d.Chosen) + len(d.Suppressed); n != len(c.wants.AsChannels()) {
				t.Errorf("the decision answers for %d channels and the notice asked for %d", n, len(c.wants.AsChannels()))
			}
		})
	}
}

// Every requested channel gets one answer, for every combination of inputs the
// type allows. This is the property delivery_ledger_coverage measures: a channel
// Decide neither chose nor suppressed is a requested row with no terminal row.
func TestDecideAnswersForEveryChannel(t *testing.T) {
	everything := contracts.Scope{Now: noon, Available: contracts.Channels}
	for _, wants := range []contracts.Wants{
		0, contracts.WantsInApp, contracts.WantsEmail, contracts.WantsPush, contracts.WantsWebPush,
		contracts.WantsWebhook, contracts.WantsAll, contracts.WantsEmail | contracts.WantsWebhook,
	} {
		for _, sender := range []*contracts.Sender{nil, {Status: contracts.SenderPending}, {Status: contracts.SenderVerified, Key: []byte("k")}} {
			d := contracts.Decide(wants, contracts.ClassStandard, "task.assigned", nil, nil, sender, everything)
			seen := map[contracts.Channel]int{}
			for _, c := range d.Chosen {
				seen[c]++
			}
			for _, s := range d.Suppressed {
				seen[s.Channel]++
				if s.Reason == "" {
					t.Errorf("%s was suppressed with no reason, and the ledger would say nothing", s.Channel)
				}
				if s.Corrects == "" {
					t.Errorf("%s was suppressed with no correction named", s.Channel)
				}
			}
			for _, c := range wants.AsChannels() {
				if seen[c] != 1 {
					t.Errorf("asks %v: channel %s appears %d times in the decision, want exactly 1", wants, c, seen[c])
				}
			}
		}
	}
}

// A suppression sentence has to survive a text column and a person reading it.
func TestSuppressionReasonsAreCompleteSentences(t *testing.T) {
	d := contracts.Decide(contracts.WantsAll, contracts.ClassStandard, "", nil, nil, nil,
		contracts.Scope{Now: noon, Available: []contracts.Channel{contracts.ChannelInApp}})
	for _, s := range d.Suppressed {
		if !strings.HasSuffix(s.Reason, ".") && s.Reason != strings.TrimSpace(s.Reason) {
			t.Errorf("%q is not a sentence an operator can read", s.Reason)
		}
		if len(s.Reason) > 500 {
			t.Errorf("%q is longer than the reason column", s.Reason)
		}
	}
}

var noon = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
