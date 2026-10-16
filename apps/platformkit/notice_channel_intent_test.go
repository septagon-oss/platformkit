package main

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
)

func TestProductNoticeChannelsPreserveEachConsumersIntent(t *testing.T) {
	for _, tc := range []struct {
		name string
		mail bool
		tell func(context.Context, db.Tx[db.Tenant], notificationcontracts.Service) error
	}{
		{"reviewed proposal", true, func(ctx context.Context, tx db.Tx[db.Tenant], notices notificationcontracts.Service) error {
			return (proposalNotifier{notices: notices}).Told(ctx, tx, changecontracts.Notice{
				Recipient: notificationtest.Ada, Kind: changecontracts.NoticeReviewed,
				State: changecontracts.StateApproved, Link: "/app/change/proposals/example",
			})
		}},
		{"applied proposal", true, func(ctx context.Context, tx db.Tx[db.Tenant], notices notificationcontracts.Service) error {
			return (proposalNotifier{notices: notices}).Told(ctx, tx, changecontracts.Notice{
				Recipient: notificationtest.Ada, Kind: changecontracts.NoticeApplied,
				Link: "/app/change/proposals/example",
			})
		}},
		{"access request", false, func(ctx context.Context, tx db.Tx[db.Tenant], notices notificationcontracts.Service) error {
			return (accessReach{notify: notices}).Tell(ctx, tx, httpx.AccessNotice{
				To: notificationtest.Ada, Requester: notificationtest.Bob,
				Permission: "task:update", RefusedPath: "/app/task/tasks",
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notices := notificationtest.NewFake()
			// The contracts fake ignores the transaction and retains the notice and
			// requested events, so this checks the product adapter's channel intent.
			var tx db.Tx[db.Tenant]
			if err := tc.tell(t.Context(), tx, notices); err != nil {
				t.Fatalf("tell: %v", err)
			}
			rows, total, err := notices.ListFor(t.Context(), tx, notificationtest.Ada, crud.Query{})
			if err != nil || total != 1 || len(rows) != 1 {
				t.Fatalf("recipient inbox = %v, total %d, error %v; want one notice", rows, total, err)
			}
			if rows[0].Title == "" || rows[0].Body == "" || rows[0].Link == "" {
				t.Errorf("notice lost its readable content or destination: %+v", rows[0])
			}
			want := []string{notificationcontracts.EventCreated}
			if tc.mail {
				want = append(want, notificationcontracts.EventEmailRequested)
			}
			if got := notices.Published(); !slices.Equal(got, want) {
				t.Errorf("requested events = %v, want %v", got, want)
			}
		})
	}
}
