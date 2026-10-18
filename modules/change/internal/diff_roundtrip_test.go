package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// TestTheRowCarriesADiffThatDigestsToTheDigestOnTheRow is the promise the whole
// object is built on, stated as something a database can refute.
//
// contracts.Diff's comment says the digest is "the digest of its canonical bytes",
// the entity says the digest is stored because "a verdict is about the digest", and
// modules/change/README.md says the digest lives in the contract package "because a
// verdict is about bytes and two implementations that disagree about which bytes were
// reviewed is the failure this object exists to prevent". So a person holding one
// row can check the two against each other: the diff the row carries has to digest
// to the digest the row carries, or the answer to "what did you approve" depends on
// which of the two columns you looked at first.
//
// The diff is written as the canonical bytes Diff.Value produces and read back
// through Postgres' jsonb, and jsonb stores a number as a numeric, not as the text
// it arrived as: it rewrites 1e2 as 100. The canonical form keeps every number as
// the text it decoded with UseNumber, so it round-trips 100 as 100 and 1.10 as 1.10
// but rewrites the exponent form and, on the way back, digests different bytes than
// the ones the digest was taken over. The submitter sent one document; the row now
// holds two spellings of it and a digest that matches neither the second nor, once
// anybody re-reads the row, the first.
//
// Every spelling below is valid JSON a client can put in a request body, which is
// why the case asks for each of them through the same door a request comes through:
// decoded into a Diff, proposed, committed, and read back with svc.Get.
func TestTheRowCarriesADiffThatDigestsToTheDigestOnTheRow(t *testing.T) {
	cases := []struct {
		name string
		text string
	}{
		{"an integer", `{"limit":100}`},
		{"a fraction that keeps its zero", `{"rate":1.10}`},
		{"an exponent", `{"limit":1e2}`},
		{"an exponent inside an object", `{"nested":{"rate":1E-2}}`},
		{"an exponent inside an array", `{"steps":[1e3,2]}`},
		{"a removal and a replacement", `{"gone":null,"limit":11}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, conn := dbtest.Schema(t, change.Migrations)
			subject := newCounter()
			svc := internal.NewService(bindingTo(subject))
			ctx := tenancy.WithTenant(context.Background(), acme)

			var id uuid.UUID
			var digestOnWrite string
			if err := db.Run(tenancy.WithActor(ctx, proposer), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				row, err := svc.Propose(ctx, tx, contracts.NewProposal{
					SubjectModule: "counter", SubjectEntity: "one",
					Diff: diff(t, tc.text), Summary: "one line",
				})
				if err != nil {
					return err
				}
				id, digestOnWrite = row.ID, row.DiffDigest
				return nil
			}); err != nil {
				t.Fatalf("propose %s: %v", tc.text, err)
			}

			// Read back in a transaction of its own, the way a reviewer reads a
			// proposal: from the row, not from what the submitter's process still holds.
			if err := db.Run(tenancy.WithActor(ctx, proposer), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				row, err := svc.Get(ctx, tx, id)
				if err != nil {
					return err
				}
				canonical, err := row.Diff.Canonical()
				if err != nil {
					return err
				}
				recomputed, err := row.Diff.Digest()
				if err != nil {
					return err
				}
				if recomputed != digestOnWrite {
					t.Errorf("the diff the row carries, %s, digests to %s, and the row's digest is %s",
						canonical, recomputed, digestOnWrite)
				}
				return nil
			}); err != nil {
				t.Fatalf("read back %s: %v", tc.text, err)
			}
		})
	}
}
