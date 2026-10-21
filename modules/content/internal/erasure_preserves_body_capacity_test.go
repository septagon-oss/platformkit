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
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

// TestErasureCanAnonymiseContentWhoseBodyAlreadyFillsItsAllowedLength writes a body
// the entity accepts at exactly its declared length, ending with the person's name,
// and erases the person: the erasure succeeds, the row survives without the name,
// and the body stays within the length the entity declares.
func TestErasureCanAnonymiseContentWhoseBodyAlreadyFillsItsAllowedLength(t *testing.T) {
	_, conn := dbtest.Schema(t, content.Migrations)
	person := personaldata.Subject{ID: uuid.New(), DisplayName: "Jo Jones"}
	limit := 0
	for _, field := range crud.Fields[*contracts.Content]() {
		if field.Name == "body" {
			limit = field.MaxLength
		}
	}
	suffix := " " + person.DisplayName
	if limit <= len(suffix) {
		t.Fatalf("body metadata has no usable capacity: %d", limit)
	}
	row := &contracts.Content{
		Slug:     "notes-" + uuid.NewString()[:8],
		Title:    "Notes",
		Body:     strings.Repeat("x", limit-len(suffix)) + suffix,
		AuthorID: uuid.New(),
	}
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, row)
	}); err != nil {
		t.Fatalf("create valid content at its declared body limit: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erasure of valid content failed: %v", err)
	}
	var kept contracts.Content
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Where("id = ?", row.ID).First(&kept).Error
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(kept.Body, person.DisplayName) {
		t.Errorf("retained body still names the person at %d of %d bytes",
			strings.LastIndex(kept.Body, person.DisplayName), len(kept.Body))
	}
	if size := utf8.RuneCountInString(kept.Body); size > limit {
		t.Errorf("anonymised body exceeds its declared capacity: %d > %d", size, limit)
	}
}
