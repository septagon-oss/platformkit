package contenttest_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
)

// An author is refused even after another person published the row; a retry
// must pass the same duty check before it can answer the stored content.
func TestTheFakeRefusesItsAuthorBeforePublicationOrRetry(t *testing.T) {
	for _, status := range []string{contracts.StatusDraft, contracts.StatusPublished} {
		t.Run(status, func(t *testing.T) {
			fake := contenttest.NewFake()
			author := uuid.New()
			tenant := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "acme"})
			writer := tenancy.WithActor(tenant, author)
			editor := tenancy.WithActor(tenant, uuid.New())
			id := fake.Put(&contracts.Content{Slug: "authored-page", Title: "Authored page", AuthorID: author})
			if status == contracts.StatusPublished {
				if _, err := fake.Publish(editor, db.Tx[db.Tenant]{}, id); err != nil {
					t.Fatal(err)
				}
			}
			before, err := fake.Content(id)
			if err != nil {
				t.Fatal(err)
			}
			events := fake.Published()
			got, err := fake.Publish(writer, db.Tx[db.Tenant]{}, id)
			if !errors.Is(err, crud.ErrConflict) || !strings.Contains(err.Error(), "author") {
				t.Errorf("author publication = %v, want a conflict naming authorship", err)
			}
			if got != nil {
				t.Errorf("refused publication returned a row: %+v", got)
			}
			after, err := fake.Content(id)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Errorf("refusal changed the row: before=%+v after=%+v error=%v", before, after, err)
			}
			if !slices.Equal(events, fake.Published()) {
				t.Errorf("refusal changed events from %v to %v", events, fake.Published())
			}
			if got, err := fake.Publish(editor, db.Tx[db.Tenant]{}, id); err != nil || got == nil {
				t.Fatalf("another person's publication = %+v, %v", got, err)
			}
			if !slices.Equal(fake.Published(), []string{contracts.EventPublished}) {
				t.Errorf("publication and retry emitted %v, want one publication", fake.Published())
			}
		})
	}
}
