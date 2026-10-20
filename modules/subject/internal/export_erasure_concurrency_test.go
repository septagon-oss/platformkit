package internal_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// pausedExport keeps the existing contributor fake's behavior, but lets an
// erasure arrive while the worker is collecting a part of the person's bundle.
type pausedExport struct {
	ownerStub
	started chan struct{}
	resume  chan struct{}
}

func (p pausedExport) Export(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) (json.RawMessage, error) {
	close(p.started)
	select {
	case <-p.resume:
		return p.ownerStub.Export(ctx, tx, s)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestAnExportRacingErasureDoesNotCommitStaleWork(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	person := personaldata.Subject{ID: uuid.New(), Email: "racing@example.com", DisplayName: "Racing Person"}
	seed(t, conn, person)
	part := pausedExport{ownerStub: ownerStub{name: "alpha", rec: &recorder{}}, started: make(chan struct{}), resume: make(chan struct{})}
	store, notes := &bundlesStub{}, &notesStub{}
	svc := internal.NewService(internal.Deps{
		People: peopleStub{}, Parts: []personaldata.Part{{Module: "alpha", Data: part}},
		Bundles: store, Notes: notes,
		Now: func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
	}).(doorSvc)
	id := ask(t, svc, conn, person)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 15*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- svc.WalkTenant(ctx, conn) }()
	select {
	case <-part.started:
	case err := <-finished:
		t.Fatalf("worker did not reach the contributor: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	erased := make(chan error, 1)
	go func() {
		eraseCtx := tenancy.WithPrincipal(tenancy.WithActor(ctx, person.ID), tenancy.Principal{
			UserID: person.ID, Permissions: []string{contracts.PermissionErase},
		})
		erased <- db.Run(eraseCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, _, err := svc.RequestErase(ctx, tx, contracts.EraseInput{Subject: person.ID, Confirm: person.Email})
			return err
		})
	}()
	// A corrected implementation may serialize erasure behind the worker's lock.
	// Release the contributor in that case too; either ordering must end erased.
	erasedFirst := false
	select {
	case err := <-erased:
		erasedFirst = true
		if err != nil {
			close(part.resume)
			<-finished
			t.Fatalf("erasure: %v", err)
		}
	case <-time.After(time.Second):
	}
	close(part.resume)
	workerErr := <-finished
	if !erasedFirst {
		if err := <-erased; err != nil {
			t.Fatalf("serialized erasure: %v", err)
		}
	}
	t.Logf("erasure committed before worker resumed: %v; worker result: %v", erasedFirst, workerErr)
	row := readRequest(t, conn, id)
	if row.State != contracts.StateErased || row.Bundle != uuid.Nil {
		t.Errorf("request after both commands: state=%s bundle=%s", row.State, row.Bundle)
	}
	if erasedFirst {
		if n := published(t, conn, contracts.EventExportReady); n != 0 {
			t.Errorf("worker committed %d stale ready events after erasure changed its expected revision", n)
		}
	}
	// The fake's Put counter is not transactional; only the committed request
	// and outbox above can distinguish rolled-back work from a stale commit.
	t.Logf("bundle calls: puts=%d discards=%d", store.puts, store.discards)
}
