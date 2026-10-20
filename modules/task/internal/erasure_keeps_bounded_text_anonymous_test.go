package internal_test

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

func TestErasureLeavesNoNameWhenABoundedTitleEndsInALongerName(t *testing.T) {
	_, conn := dbtest.Schema(t, task.Migrations)
	person := personaldata.Subject{ID: uuid.New(), DisplayName: "Jo Jones"}
	limit := 0
	for _, field := range crud.Fields[*contracts.Task]() {
		if field.Name == "title" {
			limit = field.MaxLength
		}
	}
	prefix, suffix := person.DisplayName+": ", " / Jo Joneson"
	if limit <= len(prefix)+len(suffix) {
		t.Fatalf("title metadata has no usable capacity: %d", limit)
	}
	row := &contracts.Task{Title: prefix + strings.Repeat("x", limit-len(prefix)-len(suffix)) + suffix}
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, row)
	}); err != nil {
		t.Fatalf("create a valid task at its declared title limit: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erase a person mentioned in a valid title: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		kept, err := crud.Get[*contracts.Task](tx, row.ID)
		if err != nil {
			return err
		}
		name := regexp.MustCompile(`\b` + regexp.QuoteMeta(person.DisplayName) + `\b`)
		if name.MatchString(kept.Title) {
			t.Errorf("committed erasure left the person's whole name in the title: %q", kept.Title)
		}
		return nil
	}); err != nil {
		t.Fatalf("read the surviving task and check its anonymity: %v", err)
	}
}
