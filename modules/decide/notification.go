package decide

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// MatchNotifications adapts the existing notice service to Decide's batch port.
// link must return a protected local question/option URL; invalid links refuse
// delivery. Channels and localized templates remain the composing product's.
func MatchNotifications(service notification.Service, link func(uuid.UUID, uuid.UUID) string, wants notification.Wants) (contracts.Notifier, error) {
	if service == nil || link == nil {
		return nil, fmt.Errorf("decide: match notices require a notification service and protected link")
	}
	return matchNotifications{service, link, wants}, nil
}

type matchNotifications struct {
	service notification.Service
	link    func(uuid.UUID, uuid.UUID) string
	wants   notification.Wants
}

func (n matchNotifications) Matched(ctx context.Context, tx db.Tx[db.Tenant], matched contracts.Matched) error {
	link := n.link(matched.QuestionID, matched.OptionID)
	if !httpx.LocalPath(link) {
		return fmt.Errorf("decide: match notice link must be local")
	}
	for _, recipient := range matched.Recipients {
		if _, err := n.service.Notify(ctx, tx, notification.Notice{Recipient: recipient, Intent: contracts.EventMatched,
			Class: notification.ClassStandard, Title: "An option matched", Body: "Open the decision to see the result.", Link: link, Wants: n.wants}); err != nil {
			return err
		}
	}
	return nil
}
