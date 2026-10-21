package internal_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
)

func TestTheCombinedDeckFiltersBeforeCountingAndPaging(t *testing.T) {
	w := newWorld(t)
	want := map[uuid.UUID]int64{}
	for _, c := range []struct {
		name     string
		tenant   uuid.UUID
		member   uuid.UUID
		status   string
		deadline *time.Time
		removed  *time.Time
		voter    uuid.UUID
		value    *contracts.Value
		visible  bool
	}{
		{name: "unvoted", visible: true},
		{name: "another person's ballot", voter: decidetest.Bob, value: new(contracts.No), visible: true},
		{name: "undone ballot", voter: decidetest.Alice, visible: true},
		{name: "future deadline", deadline: new(decidetest.At.Add(time.Second)), visible: true},
		{name: "not a participant", member: decidetest.Evan},
		{name: "closed", status: contracts.StatusClosed},
		{name: "deadline equality", deadline: new(decidetest.At)},
		{name: "removed option", removed: new(decidetest.At)},
		{name: "voted", voter: decidetest.Alice, value: new(contracts.Skip)},
		{name: "another tenant", tenant: uuid.New()},
	} {
		snap := decidetest.Seed()
		q, o := snap.Question, snap.Options[0]
		q.ID, q.Title, q.ClosesAt = uuid.New(), c.name, c.deadline
		q.TenantID, o.TenantID = uuid.Nil, uuid.Nil
		if c.status != "" {
			q.Status, q.ClosedAt = c.status, new(decidetest.At)
		}
		o.ID, o.QuestionID, o.Position, o.RemovedAt = uuid.New(), q.ID, 1, c.removed
		ctx := w.ctx
		if c.tenant != uuid.Nil {
			ctx = tenancy.WithTenant(ctx, tenancy.Tenant{ID: c.tenant})
		}
		if err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := crud.Create(ctx, tx, &q); err != nil {
				return err
			}
			if err := crud.Create(ctx, tx, &o); err != nil {
				return err
			}
			for _, p := range snap.Participants {
				p.ID, p.QuestionID, p.TenantID = uuid.New(), q.ID, uuid.Nil
				if c.member != uuid.Nil && p.PersonID == decidetest.Alice {
					p.PersonID = c.member
				}
				if err := crud.Create(ctx, tx, &p); err != nil {
					return err
				}
			}
			if c.voter != uuid.Nil {
				return crud.Create(ctx, tx, &contracts.Vote{Base: entity.Base{ID: uuid.New()},
					QuestionID: q.ID, OptionID: o.ID, PersonID: c.voter, Value: c.value, Revision: 2, At: decidetest.At})
			}
			return nil
		}); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if c.visible {
			want[o.ID] = 0
			if c.voter == decidetest.Alice {
				want[o.ID] = 2
			}
		}
	}
	_, handler := mountedDecide(t, w)
	ids := make([]uuid.UUID, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	for offset := range len(ids) + 1 {
		resp := request(t, handler, http.MethodGet, fmt.Sprintf("/api/v1/decide/cards?filter=unvoted:true&sort=position&limit=1&offset=%d", offset), "")
		var page struct {
			Items []contracts.Card `json:"items"`
			Total int64            `json:"total"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &page); err != nil || resp.Code != http.StatusOK {
			t.Fatalf("page %d: %d %s %v", offset, resp.Code, resp.Body.String(), err)
		}
		if page.Total != int64(len(ids)) {
			t.Fatalf("page %d total: %+v", offset, page)
		}
		if offset == len(ids) {
			if len(page.Items) != 0 {
				t.Fatalf("past the last card: %+v", page)
			}
			continue
		}
		if len(page.Items) != 1 || page.Items[0].ID != ids[offset] || !page.Items[0].Unvoted || page.Items[0].VoteRevision != want[ids[offset]] {
			t.Fatalf("page %d: %+v; want %s revision %d", offset, page, ids[offset], want[ids[offset]])
		}
	}
	outsider := uuid.New()
	if _, err := w.admin.ExecContext(t.Context(), `INSERT INTO decide_test_people VALUES ($1,$2,true,true,true,true)`, decidetest.Tenant, outsider); err != nil {
		t.Fatal(err)
	}
	w.ctx = tenancy.WithPrincipal(w.ctx, tenancy.Principal{UserID: outsider})
	empty := request(t, handler, http.MethodGet, "/api/v1/decide/cards?filter=unvoted:true&sort=position", "")
	var page struct {
		Items []contracts.Card `json:"items"`
		Total int64            `json:"total"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &page); err != nil || empty.Code != http.StatusOK || page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("nonparticipant queue: %d %s %v", empty.Code, empty.Body.String(), err)
	}
	if _, err := w.admin.ExecContext(t.Context(), `UPDATE decide_test_people SET vote=false WHERE tenant_id=$1 AND id=$2`, decidetest.Tenant, outsider); err != nil {
		t.Fatal(err)
	}
	denied := request(t, handler, http.MethodGet, "/api/v1/decide/cards?filter=unvoted:true&sort=position", "")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("revoked vote grant: %d %s", denied.Code, denied.Body.String())
	}
}

func TestExportOmitsRemovedOptionsAndTheirBallotsTogether(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := put(ctx, tx, decidetest.Seed()); err != nil {
			return err
		}
		for _, id := range []uuid.UUID{decidetest.A, decidetest.B} {
			if _, err := w.svc.Vote(ctx, tx, contracts.VoteInput{QuestionID: decidetest.Q, OptionID: id, Value: contracts.Yes}); err != nil {
				return err
			}
		}
		_, err := w.svc.RemoveOption(ctx, tx, contracts.RemoveOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: decidetest.Q, IfRevision: 1}, OptionID: decidetest.A})
		return err
	})
	_, handler := mountedDecide(t, w)
	resp := request(t, handler, http.MethodGet, "/api/v1/decide/questions/"+decidetest.Q.String()+"/export", "")
	var out contracts.Export
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil || resp.Code != http.StatusOK {
		t.Fatalf("export: %d %s %v", resp.Code, resp.Body.String(), err)
	}
	if len(out.Options) != 2 || len(out.Results.Options) != 2 || len(out.Votes) != 1 || out.Votes[0].OptionID != decidetest.B {
		t.Fatalf("current rows: %+v", out)
	}
	var ballots int
	if err := w.admin.QueryRowContext(t.Context(), `SELECT count(*) FROM decide_votes`).Scan(&ballots); err != nil || ballots != 2 {
		t.Fatalf("export changed retained ballots: %d %v", ballots, err)
	}
}
