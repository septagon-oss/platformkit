package change_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

// stubSubject is the smallest thing that satisfies contracts.Subject. The case
// below is about whether the two port signatures a consumer has to write are the
// ones the contract declares, and a subject that does nothing says that without
// saying anything about a row this module does not own.
type stubSubject struct{}

func (stubSubject) Lock(context.Context, db.Tx[db.Tenant]) (json.RawMessage, int64, error) {
	return json.RawMessage(`{}`), 0, nil
}

func (stubSubject) Save(context.Context, db.Tx[db.Tenant], json.RawMessage) (int64, error) {
	return 0, nil
}

// TestTheCompositionSnippetIsTheContract is the two halves of the promise a module
// README exists for, checked rather than asserted in prose.
//
// The first half is README.md's Composition literal, transcribed with the
// application's subject replaced by a stub: a reader who types five lines out of a
// document and gets `unknown field New in struct literal` is not composing
// anything, and the field names and the Resolve signature are exactly the things a
// binding author cannot guess from the sentence above them. That this compiles is
// the assertion, and Module and NewService are what stop a reader finding out at
// boot that the snippet composes neither a manifest nor a usable service.
//
// The second half is contracts.Refusal, the answer a gated door gives: the
// composition in apps/platformkit wraps it in crud.ErrConflict and hands it to
// kit/rest's Fault, so the only part of it a person reads is its Error(). If that
// sentence stopped naming the door and the grant, the refusal would be a wall —
// the thing change control exists not to be.
func TestTheCompositionSnippetIsTheContract(t *testing.T) {
	deps := change.Deps{Subjects: []contracts.SubjectBinding{{
		Module: "site", Entity: "settings",
		Resolve: func(ctx context.Context, tx db.Tx[db.Tenant], subjectID uuid.UUID) (contracts.Subject, error) {
			return stubSubject{}, nil
		},
	}}}
	svc := change.NewService(deps.Subjects)
	if m := change.New(deps); svc == nil || m.Name != "change" || len(m.Permissions) != 3 {
		t.Fatal("the composition in README.md builds no service, or no manifest carrying the three keys it declares")
	}

	refusal := (&contracts.Refusal{
		SubjectModule: "site", SubjectEntity: "settings", SubjectID: uuid.Nil,
		Path: "/api/v1/change/proposals", Permission: contracts.PermissionChangePropose,
	}).Error()
	for _, want := range []string{"/api/v1/change/proposals", contracts.PermissionChangePropose} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal %q never says %q, so the caller was not told the way through", refusal, want)
		}
	}
}
