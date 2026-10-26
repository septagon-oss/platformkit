package savedtest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	picture "github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
)

func TestFakeConforms(t *testing.T) {
	savedtest.RunService(t, func(t *testing.T, run func(savedtest.Fixture)) {
		at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		source := &savedtest.Source{Value: contracts.Capture{Title: "First", Locale: "en", Version: "v1", Values: json.RawMessage(`{"resolved":["first"]}`)}, Destination: "/app/example/things/one"}
		fake := savedtest.NewFake(at, []contracts.Savable{{Module: "example", Kind: "thing", Source: source}, {Module: "example", Kind: "plan", Source: source}})
		tenant, otherTenant := tenancy.Tenant{ID: uuid.New()}, tenancy.Tenant{ID: uuid.New()}
		person, otherPerson, denied := uuid.New(), uuid.New(), uuid.New()
		as := func(tenant tenancy.Tenant, person uuid.UUID) context.Context {
			return tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant), tenancy.Principal{UserID: person})
		}
		owner, other := as(tenant, person), as(otherTenant, person)
		fake.Grants.Grant(person, contracts.PermissionRead, contracts.PermissionWrite)
		fake.Grants.Grant(otherPerson, contracts.PermissionRead, contracts.PermissionWrite)
		run(savedtest.Fixture{Owner: owner, OtherPerson: as(tenant, otherPerson), OtherTenant: other, Denied: as(tenant, denied),
			Service: fake, Undeclared: fake.WithoutSources(), Source: source, Clock: fake.Clock,
			Within: func(_ context.Context, run func(db.Tx[db.Tenant]) error) error { return run(db.Tx[db.Tenant]{}) },
			Rows: func() string {
				rows := append(fake.Rows(owner), fake.Rows(other)...)
				return fmt.Sprintf("%#v", rows)
			},
			Published:       fake.Events.Names,
			Revoke:          func() { fake.Grants = &porttest.Grants{} },
			Picture:         retainedPicture(t),
			ComposedPicture: picturetest.Oracle,
		})
	})
}

// retainedPicture compiles the picture the suite's retention case saves and reads
// back. The fixture bytes belong to the picture module, and shipped source may not
// import that helper, so the world that runs the suite brings the composition.
func retainedPicture(t *testing.T) contracts.PictureInput {
	t.Helper()
	template, err := picture.Compile("fixture", picturetest.SVG)
	if err != nil {
		t.Fatal(err)
	}
	return contracts.PictureInput{Template: template, Value: picturetest.Value()}
}
