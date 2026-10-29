package notification_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// selfCarrier names a channel this module carries itself, so the refusal is reached
// without a provider that sends anything; its methods are never called.
type selfCarrier struct{ c contracts.Channel }

func (p selfCarrier) Channel() contracts.Channel { return p.c }

func (p selfCarrier) Deliver(context.Context, db.Tx[db.Tenant], contracts.Delivery) error {
	return nil
}

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: a second carrier on in-app or mail is
// two subscriptions on one event and two sent rows for one delivery.
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps notification.Deps
		want string
	}{
		{"a provider for the in-app row", notification.Deps{Providers: contracts.Providers{contracts.ChannelInApp: selfCarrier{contracts.ChannelInApp}}}, "Deps.Providers carries a channel this module carries itself: in_app"},
		{"a provider for the mail this module sends", notification.Deps{Providers: contracts.Providers{contracts.ChannelEmail: selfCarrier{contracts.ChannelEmail}}}, "Deps.Providers carries a channel this module carries itself: email"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, _, err := notification.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked carried one channel twice")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				notification.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: no provider is a deployment
// that sends nothing but the row, and every notice it is asked for is accounted for
// as suppressed with a reason.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := notification.NewChecked(notification.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if svc == nil {
		t.Fatal("the notification service is nil")
	}
	if m.Name != "notification" {
		t.Errorf("manifest name = %q, want notification", m.Name)
	}
	if len(m.Subscriptions) != 1 {
		t.Errorf("subscriptions = %d, want the mail carrier alone", len(m.Subscriptions))
	}
}
