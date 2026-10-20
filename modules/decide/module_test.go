package decide_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	notification "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/pkit"
)

type access struct{}

func (access) Current(context.Context, db.Tx[db.Tenant], []uuid.UUID) (contracts.Actor, error) {
	return contracts.Actor{}, contracts.ErrDenied
}

type tenants struct{}

func (tenants) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return nil, nil }

type notifier struct{}

func (notifier) Matched(context.Context, db.Tx[db.Tenant], contracts.Matched) error { return nil }

func providers(key contracts.InviteKey) *pkit.Module {
	return pkit.NewModule("decide_fixture", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[contracts.Access](w, access{})
		pkit.Put[contracts.Clock](w, func() time.Time { return time.Unix(1900000000, 0).UTC() })
		pkit.Put(w, key)
		pkit.Put[contracts.Notifier](w, notifier{})
		pkit.Put[jobs.TenantLister](w, tenants{})
		pkit.Put[rest.FileUses](w, rest.RecordNoUses{})
		pkit.Put[rest.FileShown](w, rest.ShowNothing{})
		pkit.Put[richtext.Files](w, richtext.RejectImages{})
		return module.Module{Name: "decide_fixture"}, nil
	}, pkit.Provides[contracts.Access](), pkit.Provides[contracts.Clock](), pkit.Provides[contracts.InviteKey](), pkit.Provides[contracts.Notifier](),
		pkit.Provides[jobs.TenantLister](), pkit.Provides[rest.FileUses](), pkit.Provides[rest.FileShown](), pkit.Provides[richtext.Files]())
}

func TestCompositionRequiresCurrentAuthorityClockAndValidKey(t *testing.T) {
	d := pkit.Deployment{Environment: pkit.Development}
	if err := pkit.NewApp("decision-fixture").Use(decide.Module).Validate(d); err == nil || !strings.Contains(err.Error(), "Access") || !strings.Contains(err.Error(), "Clock") {
		t.Fatalf("missing providers: %v", err)
	}
	key := contracts.InviteKey("isolated-composition-test-key-32-bytes")
	if err := pkit.NewApp("decision-fixture").Use(providers(key), decide.Module).Validate(d); err != nil {
		t.Fatal(err)
	}
	if err := pkit.NewApp("decision-fixture").Use(providers(nil), decide.Module).Validate(d); err == nil {
		t.Fatal("empty key accepted")
	}
	if err := pkit.NewApp("decision-fixture").Use(providers(key), decide.WithSweepEvery(-time.Second)).Validate(d); err == nil {
		t.Fatal("negative sweep accepted")
	}
}

func TestMatchesUseTheExistingNotificationPortForEveryRecipient(t *testing.T) {
	service := notificationtest.NewFake()
	n, err := decide.MatchNotifications(service, func(uuid.UUID, uuid.UUID) string { return "/app/decisions/result" }, notification.Wants(0))
	if err != nil {
		t.Fatal(err)
	}
	matched := contracts.Matched{QuestionID: uuid.New(), OptionID: uuid.New(), Recipients: []uuid.UUID{uuid.New(), uuid.New()}}
	if err := n.Matched(t.Context(), db.Tx[db.Tenant]{}, matched); err != nil {
		t.Fatal(err)
	}
	for _, recipient := range matched.Recipients {
		rows, total, err := service.ListFor(t.Context(), db.Tx[db.Tenant]{}, recipient, crud.Query{})
		if err != nil || total != 1 || rows[0].Link != "/app/decisions/result" {
			t.Fatalf("notice: %+v %d %v", rows, total, err)
		}
	}
}
