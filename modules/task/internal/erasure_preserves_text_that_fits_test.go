package internal_test

import (
	"context"
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

func TestErasurePreservesOtherTextWhenTheCompactTitleFits(t *testing.T) {
	_, conn := dbtest.Schema(t, task.Migrations)
	person := personaldata.Subject{ID: uuid.New(), DisplayName: "Jo Jones"}
	limit := 0
	for _, field := range crud.Fields[*contracts.Task]() {
		if field.Name == "title" {
			limit = field.MaxLength
		}
	}
	prefix, suffix := person.DisplayName+": ", " / assigned to Maria Gomez"
	if limit <= len(prefix)+len(suffix)+3 {
		t.Fatalf("title metadata has no usable capacity: %d", limit)
	}
	row := &contracts.Task{Title: prefix + strings.Repeat("x", limit-len(prefix)-len(suffix)-3) + suffix}
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, row)
	}); err != nil {
		t.Fatalf("create a valid task near its declared title limit: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erase a person mentioned in the title: %v", err)
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		kept, err := crud.Get[*contracts.Task](tx, row.ID)
		if err != nil {
			return err
		}
		if strings.Contains(kept.Title, person.DisplayName) {
			t.Errorf("committed title still names the erased person: %q", kept.Title)
		}
		if !strings.HasSuffix(kept.Title, suffix) {
			t.Errorf("erasure removed unrelated text although the compact title fits: %q, want suffix %q", kept.Title, suffix)
		}
		return nil
	}); err != nil {
		t.Fatalf("read the committed task: %v", err)
	}
}
