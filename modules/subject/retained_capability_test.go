package subject

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

type retainedOnly struct{ fake *usertest.Fake }

func (r retainedOnly) Retained(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*usercontracts.User, error) {
	return r.fake.Retained(ctx, tx, id)
}

func TestExportReadsRetainedCapabilityAndPrefersItToLegacyPeople(t *testing.T) {
	fake := usertest.NewFake()
	ctx, tx := t.Context(), db.Tx[db.Tenant]{}
	person, err := fake.Invite(ctx, tx, "retained@example.test", "Retained person")
	if err != nil {
		t.Fatal(err)
	}
	if err := fake.Delete(ctx, person.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := fake.Get(ctx, tx, person.ID); err == nil {
		t.Fatal("deleted person is visible through Get")
	}
	deps := Deps{Subjects: retainedOnly{fake}, People: usertest.NewFake()}
	svc := internal.NewService(internal.Deps{People: subjects(deps)})
	bundle, err := svc.Export(ctx, tx, person.ID)
	if err != nil || bundle == nil {
		t.Fatalf("retained export = %v, %v", bundle, err)
	}
	if subjects(Deps{People: fake}) != fake {
		t.Fatal("legacy fallback was lost")
	}
}
