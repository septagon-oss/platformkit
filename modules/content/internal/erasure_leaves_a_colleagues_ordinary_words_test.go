package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

// TestErasingAPersonWhoseHandleIsAnOrdinaryWordLeavesOtherPeoplesSentences erases a
// person whose handle is "will" — a handle users_handle_form accepts — from a tenant
// where a colleague wrote a post that uses the word as a verb and never mentions the
// person. The colleague's sentence is not the person's data and has to survive the
// erasure as written, while the same erasure still finds the person's own address in
// a post that does name them.
func TestErasingAPersonWhoseHandleIsAnOrdinaryWordLeavesOtherPeoplesSentences(t *testing.T) {
	_, conn := dbtest.Schema(t, content.Migrations)
	in := tenancy.WithTenant(t.Context(), acme)
	subject := personaldata.Subject{ID: uuid.New(), Email: "will.turner@example.com", DisplayName: "Will Turner", Handle: "will"}
	plan, handover := uuid.New(), uuid.New()
	const sentence = "The deploy will run on Monday, and the retro will follow on Tuesday."

	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		for _, post := range []struct {
			id                uuid.UUID
			slug, title, body string
		}{
			{plan, "release-plan", "Release plan", sentence},
			{handover, "handover", "Handover", "Ask will.turner@example.com for the keys."},
		} {
			if err := tx.DB().Exec(`INSERT INTO contents (id, tenant_id, slug, title, body, author_id) VALUES (?, ?, ?, ?, ?, ?)`,
				post.id, acme.ID, post.slug, post.title, post.body, uuid.New()).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}

	var planBody, handoverBody string
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw(`SELECT body FROM contents WHERE id = ?`, plan).Scan(&planBody).Error; err != nil {
			return err
		}
		return tx.DB().Raw(`SELECT body FROM contents WHERE id = ?`, handover).Scan(&handoverBody).Error
	}); err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if planBody != sentence {
		t.Errorf("the colleague's sentence was rewritten by an erasure it does not concern:\n got  %q\n want %q", planBody, sentence)
	}
	if strings.Contains(strings.ToLower(handoverBody), subject.Email) {
		t.Errorf("the post that names the person still carries their address: %q", handoverBody)
	}
}
