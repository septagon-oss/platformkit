package authtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

type noticed struct{ links []string }

func (n *noticed) Notify(_ context.Context, _ db.Tx[db.Tenant], in notificationcontracts.Notice) (*notificationcontracts.Notification, error) {
	n.links = append(n.links, in.Link)
	return &notificationcontracts.Notification{}, nil
}

type mailed struct{ bodies []string }

func (m *mailed) Send(_ context.Context, msg notificationcontracts.Message) error {
	m.bodies = append(m.bodies, msg.Body)
	return nil
}

// TestTheFakesInvitationLeadsWhereTheServicesDoes: the real service now mails and
// notices the set-password page at the workspace address the shell serves
// (httpx.Workspace("/auth/reset")). The fake is the stand-in every consumer's tests
// read a link out of, so its notice and its mail lead to the same page.
func TestTheFakesInvitationLeadsWhereTheServicesDoes(t *testing.T) {
	ctx := context.Background()
	var tx db.Tx[db.Tenant]
	users := usertest.NewFake()
	invited, err := users.Invite(ctx, tx, "invited@example.com", "Invited")
	if err != nil {
		t.Fatal(err)
	}
	fake := authtest.NewFake(users)
	notice, mail := &noticed{}, &mailed{}
	fake.Notify, fake.Mailer = notice, mail
	if err := fake.Offer(ctx, tx, invited.ID); err != nil {
		t.Fatalf("offer: %v", err)
	}
	page := httpx.Workspace("/auth/reset")
	if len(notice.links) != 1 || notice.links[0] != page {
		t.Errorf("the fake's notice links %v, want the set-password page %s", notice.links, page)
	}
	if len(mail.bodies) != 1 || !strings.Contains(mail.bodies[0], page+"?token=") {
		t.Errorf("the fake's mail does not lead to %s:\n%v", page, mail.bodies)
	}
}
