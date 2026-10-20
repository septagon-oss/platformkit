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

// TestErasureKeepsTheTitleWholeWheneverTheCompactRewriteFits walks titles from well inside
// the declared limit up to the limit itself, each opening with the person's name and ending
// with somebody else's, and erases the person once. Every title must lose the name and stay
// inside the limit; every title whose rewrite with the narrowest placeholder the erasure used
// still fits must keep its unrelated ending whole, up to and including a rewrite that fills
// the limit exactly. The placeholder width is read from the rows themselves, so the case
// holds whatever the placeholders are spelled.
func TestErasureKeepsTheTitleWholeWheneverTheCompactRewriteFits(t *testing.T) {
	_, conn := dbtest.Schema(t, task.Migrations)
	person := personaldata.Subject{ID: uuid.New(), DisplayName: "Jo Jones"}
	limit := 0
	for _, field := range crud.Fields[*contracts.Task]() {
		if field.Name == "title" {
			limit = field.MaxLength
		}
	}
	prefix, suffix := person.DisplayName+": ", " / assigned to Maria Gomez"
	const span = 24
	if limit <= len(prefix)+len(suffix)+span {
		t.Fatalf("title metadata has no usable capacity: %d", limit)
	}

	ctx := tenancy.WithTenant(t.Context(), acme)
	rows := make(map[int]*contracts.Task, span+1)
	for length := limit - span; length <= limit; length++ {
		row := &contracts.Task{Title: prefix + strings.Repeat("x", length-len(prefix)-len(suffix)) + suffix}
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return crud.Create(ctx, tx, row)
		}); err != nil {
			t.Fatalf("create a valid %d-character task title: %v", length, err)
		}
		rows[length] = row
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erase a person named in every title: %v", err)
	}

	kept := make(map[int]string, len(rows))
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		for length, row := range rows {
			got, err := crud.Get[*contracts.Task](tx, row.ID)
			if err != nil {
				return err
			}
			kept[length] = got.Title
		}
		return nil
	}); err != nil {
		t.Fatalf("read the committed tasks: %v", err)
	}

	// The placeholder each row took is what stands before the ": " the title opened with;
	// the narrowest one seen is the compact rewrite the erasure had available.
	compact := 0
	for length, title := range kept {
		if strings.Contains(title, person.DisplayName) {
			t.Errorf("the %d-character title still names the erased person: %q", length, title)
		}
		if n := utf8.RuneCountInString(title); n > limit {
			t.Errorf("the %d-character title was rewritten to %d characters, over its limit of %d", length, n, limit)
		}
		at := strings.Index(title, ": ")
		if at <= 0 {
			t.Fatalf("the %d-character title lost the separator after its placeholder: %q", length, title)
		}
		if w := utf8.RuneCountInString(title[:at]); compact == 0 || w < compact {
			compact = w
		}
	}
	name := utf8.RuneCountInString(person.DisplayName)
	for length := limit - span; length <= limit; length++ {
		if length-name+compact > limit {
			continue
		}
		if !strings.HasSuffix(kept[length], suffix) {
			t.Errorf("the %d-character title's %d-character compact rewrite fits a limit of %d, but the erasure cut it: %q, want suffix %q",
				length, length-name+compact, limit, kept[length], suffix)
		}
	}
}
