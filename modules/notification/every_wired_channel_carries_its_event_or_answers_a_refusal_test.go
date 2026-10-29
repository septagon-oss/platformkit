package notification_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// everyChannelProvider answers for a channel the deployment wired, whatever it is.
type everyChannelProvider struct{ c contracts.Channel }

func (p everyChannelProvider) Channel() contracts.Channel { return p.c }
func (p everyChannelProvider) Deliver(context.Context, db.Tx[db.Tenant], contracts.Delivery) error {
	return nil
}

// TestEveryChannelADeploymentCanWireCarriesItsEventOrIsRefusedByName walks the
// channel vocabulary a deployment can wire and asks the question the manifest
// cannot answer twice: a channel this module carries itself (in-app is the row,
// mail is its own carrier) is refused by name, and every other channel is answered
// by the carrier subscription whose event is the one contracts.RequestedEvent names
// for it. The two tables — contracts.Channels and contracts.RequestedEvent — are
// written apart, so a channel in the one and not the other is a wiring mistake.
// What the constructor must never do is answer either case by panicking:
// internal.Carrier refuses an unknown channel by panicking, and Deps.Providers is
// how a deployment reaches it.
func TestEveryChannelADeploymentCanWireCarriesItsEventOrIsRefusedByName(t *testing.T) {
	for _, ch := range contracts.Channels {
		answers := func() (subs int, refusal string) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("NewChecked with a provider on %q panicked instead of answering: %v", ch, r)
				}
			}()
			_, m, err := notification.NewChecked(notification.Deps{
				Providers: contracts.Providers{ch: everyChannelProvider{c: ch}},
			})
			if err != nil {
				return 0, err.Error()
			}
			return len(m.Subscriptions), ""
		}
		subs, refusal := answers()
		if ch == contracts.ChannelInApp || ch == contracts.ChannelEmail {
			if !strings.Contains(refusal, "carries a channel this module carries itself: "+string(ch)) {
				t.Errorf("channel %q: refusal = %q, want it to name the channel", ch, refusal)
			}
			continue
		}
		if refusal != "" {
			t.Errorf("NewChecked with a provider on %q: %v", ch, refusal)
			continue
		}
		// The mail carrier this module always carries, plus this channel's own.
		if want := contracts.RequestedEvent[ch]; want == "" {
			t.Errorf("channel %q is wired but no event asks a worker for it", ch)
		} else if subs != 2 {
			t.Errorf("channel %q: %d subscriptions, want the mail carrier and %s", ch, subs, want)
		}
	}
}
