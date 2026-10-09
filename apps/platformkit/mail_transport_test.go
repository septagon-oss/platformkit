package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// keepMailInTheProcess is how a case says what it is about to read: a message
// this application kept in its own process rather than sent.
//
// Nothing is asked for, and that is the fact the case is testing rather than one
// it is setting up. A development deployment that names no mail server is answered
// by the notification module's simulated mailbox — the resolver refuses that
// implementation anywhere a real one could be stood up, which is the difference
// between "this machine has no relay today" and "this installation accepted a
// sign-up whose confirmation nobody will ever receive" — so the configuration
// every case here boots with already keeps its messages in memory. What this line
// guards is the premise: a server named in the file would move the pick to SMTP,
// and the case would be reading a mailbox nothing writes to.
func keepMailInTheProcess(t *testing.T, path string, cfg config.Config) (string, config.Config) {
	t.Helper()
	if cfg.Mail.Enabled() {
		t.Fatalf("%s names a mail server (%s:%d); this case reads the message out of this process's memory, which is what a deployment that names no server keeps there",
			path, cfg.Mail.Host, cfg.Mail.Port)
	}
	return path, cfg
}

// TestTheSenderAMaillessDeploymentComposesIsItsEnvironment pins the two answers
// one absent mail server gets, and they are opposite: the development deployment
// the whole suite boots keeps every message where a case can read it, and the
// production one composes the sender that refuses every send — which is what makes
// email_delivery_unavailable_test.go's reasoned 503, rather than a filed message
// nobody will ever read, the fate of a sign-up at an installation with no relay.
func TestTheSenderAMaillessDeploymentComposesIsItsEnvironment(t *testing.T) {
	_, cfg := configure(t)
	if cfg.Mail.Enabled() {
		t.Fatal("the example configuration names a mail server; this case is about the deployment that does not")
	}

	dev := composeReference(cfg, pkit.Development).mail
	if _, ok := dev.(*notification.Mailbox); !ok {
		t.Errorf("a development deployment with no mail server mails through %T; the case that reads a link out of this process's memory depends on the mailbox", dev)
	}

	prod := composeReference(cfg, pkit.Production).mail
	if !notificationcontracts.IsNoTransport(prod) {
		t.Errorf("a production deployment with no mail server mails through %T; a mailbox nobody can read is why this state exists", prod)
	}
}
