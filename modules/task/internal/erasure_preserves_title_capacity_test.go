package internal_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

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

func TestErasureCanAnonymiseATaskWhoseTitleAlreadyFillsItsAllowedLength(t *testing.T) {
	_, conn := dbtest.Schema(t, task.Migrations)
	person := personaldata.Subject{ID: uuid.New(), DisplayName: "Jo Jones"}
	limit := 0
	for _, field := range crud.Fields[*contracts.Task]() {
		if field.Name == "title" {
			limit = field.MaxLength
		}
	}
	suffix := " " + person.DisplayName
	if limit <= len(suffix) {
		t.Fatalf("title metadata has no usable capacity: %d", limit)
	}
	row := &contracts.Task{Title: strings.Repeat("x", limit-len(suffix)) + suffix}
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, row)
	}); err != nil {
		t.Fatalf("create a valid task at its declared title limit: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erasure of a valid task failed: %v", err)
	}
	var kept contracts.Task
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Where("id = ?", row.ID).First(&kept).Error
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(kept.Title, person.DisplayName) {
		t.Errorf("retained title still names the person: %q", kept.Title)
	}
	if size := utf8.RuneCountInString(kept.Title); size > limit {
		t.Errorf("anonymised title exceeds its declared capacity: %d > %d", size, limit)
	}
}
