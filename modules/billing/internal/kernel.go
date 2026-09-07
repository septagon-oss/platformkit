package internal

import (
	"context"
	"errors"
	"slices"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/billing/contracts"
)

// kernel.go is what this module answers for the kernel, the way
// modules/auth answers the permission question: one method, one seam, and no
// route of its own.

// Includes reports whether this tenant's plan includes the named feature. It is
// httpx.Entitler, asked by the middleware for every operation whose declaration
// names a feature.
//
// A tenant that has never subscribed includes nothing, which is the honest
// answer and not an error: an installation may sell to some tenants and not
// others. A cancelled subscription includes nothing either. Trial, active and
// past-due all do, because all three are periods being served — what is owed
// while past due is owed for a period that was served, and taking the product
// away the moment a card fails is how a payment retry becomes a support ticket.
//
// The read is the request's own transaction, so it is a tenant-scoped query
// under row-level security: this asks about the caller's tenant because it
// cannot reach another.
func (s *Service) Includes(ctx context.Context, _ tenancy.Tenant, feature string) (bool, error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return false, errors.New("billing: no transaction to read the subscription in")
	}
	sub, err := s.Current(ctx, tx)
	if errors.Is(err, crud.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !contracts.Serving(sub.Status) {
		return false, nil
	}
	plan, err := crud.Get[*contracts.Plan](tx, sub.PlanID)
	if err != nil {
		// A subscription to a plan that is gone is a broken row, not a
		// judgement about what the tenant may do.
		return false, err
	}
	return slices.Contains(plan.Features, feature), nil
}

var _ httpx.Entitler = (*Service)(nil)
