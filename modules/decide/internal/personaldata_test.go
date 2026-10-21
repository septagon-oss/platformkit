package internal_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
	"github.com/septagon-oss/platformkit/modules/decide/internal"
)

func TestPersonalExportKeepsOnlyTheSubjectsBallotsInItsTenant(t *testing.T) {
	w := newWorld(t)
	part := internal.PersonalData()
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := put(ctx, tx, decidetest.Seed()); err != nil {
			return err
		}
		for _, person := range []uuid.UUID{decidetest.Alice, decidetest.Bob} {
			actor := tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: person})
			if _, err := w.svc.Vote(actor, tx, contracts.VoteInput{QuestionID: decidetest.Q, OptionID: decidetest.A, Value: contracts.Yes}); err != nil {
				return err
			}
		}
		return nil
	})
	for _, tenant := range []uuid.UUID{decidetest.Tenant, uuid.New()} {
		ctx := tenancy.WithTenant(w.ctx, tenancy.Tenant{ID: tenant})
		if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			raw, err := part.Data.Export(ctx, tx, personaldata.Subject{ID: decidetest.Alice})
			if err != nil {
				return err
			}
			var out struct {
				Questions    []contracts.Question
				Options      []contracts.Option
				Participants []contracts.Participant
				Votes        []contracts.Vote
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				return err
			}
			if tenant != decidetest.Tenant {
				if len(out.Questions)+len(out.Options)+len(out.Participants)+len(out.Votes) != 0 {
					t.Fatalf("foreign tenant export: %s", raw)
				}
			} else if len(out.Questions) != 1 || len(out.Options) != 3 || len(out.Participants) != 1 || len(out.Votes) != 1 || out.Votes[0].PersonID != decidetest.Alice {
				t.Fatalf("personal export: %s", raw)
			}
			for _, hidden := range []string{"inviteGeneration", "inviteExpiresAt", "tenantId", "undoToken"} {
				if strings.Contains(string(raw), hidden) {
					t.Fatalf("personal export includes %s", hidden)
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPersonalErasureScrubsSharedTextWithRevisionsAndRollsBack(t *testing.T) {
	w := newWorld(t)
	subject := personaldata.Subject{ID: decidetest.Alice, DisplayName: "Alice Example"}
	part := internal.PersonalData()
	var shared uuid.UUID
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		snap := decidetest.Seed()
		snap.Question.Title, snap.Question.Description = subject.DisplayName, subject.DisplayName
		snap.Options[0].Title, snap.Options[0].Note = subject.DisplayName, subject.DisplayName
		if err := put(ctx, tx, snap); err != nil {
			return err
		}
		if _, err := w.svc.Vote(ctx, tx, contracts.VoteInput{QuestionID: decidetest.Q, OptionID: decidetest.A, Value: contracts.Yes}); err != nil {
			return err
		}
		other, err := w.svc.Open(tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: decidetest.Bob}), tx,
			contracts.OpenInput{Title: subject.DisplayName, Options: []contracts.OptionInput{{Title: subject.DisplayName}}})
		shared = other.ID
		return err
	})
	stop := errors.New("rollback erasure")
	if err := db.Run(w.ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := part.Data.Erase(ctx, tx, subject); err != nil {
			return err
		}
		return stop
	}); !errors.Is(err, stop) {
		t.Fatal(err)
	}
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before, err := loaded(tx, decidetest.Q)
		if err != nil || before.Question.Title != subject.DisplayName || before.Question.Revision != 1 {
			t.Fatalf("rollback lost original: %+v %v", before, err)
		}
		return nil
	})
	foreign := tenancy.WithTenant(w.ctx, tenancy.Tenant{ID: uuid.New()})
	if err := db.Run(foreign, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return part.Data.Erase(ctx, tx, subject) }); err != nil {
		t.Fatal(err)
	}
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before, err := loaded(tx, decidetest.Q)
		if err != nil || before.Question.Title != subject.DisplayName {
			t.Fatalf("foreign erasure changed content: %+v %v", before, err)
		}
		return part.Data.Erase(ctx, tx, subject)
	})
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, id := range []uuid.UUID{decidetest.Q, shared} {
			after, err := loaded(tx, id)
			if err != nil || after.Question.Revision != 2 || after.Question.Title != subject.Token() || after.Options[0].Revision != 2 || after.Options[0].Title != subject.Token() {
				t.Fatalf("scrubbed record: %+v %v", after, err)
			}
			if id == decidetest.Q && (len(after.Votes) != 1 || after.Votes[0].Value == nil || *after.Votes[0].Value != contracts.Yes || after.Votes[0].Revision != 1 || after.Options[0].FirstMatchedAt == nil) {
				t.Fatalf("erasure changed ballot or match evidence: %+v", after)
			}
		}
		_, err := w.svc.AddOption(ctx, tx, contracts.AddOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: decidetest.Q, IfRevision: 1}, Option: contracts.OptionInput{Title: "New"}})
		if !errors.Is(err, contracts.ErrStale) {
			t.Fatalf("pre-erasure revision accepted: %v", err)
		}
		return nil
	})
}
