package subject

import (
	"bytes"
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	subjectcontracts "github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// Module is a person's two doors as the resolver sees them, for an application that
// names it in Use: `Use(subject.Module)`.
//
// This is the line that makes the eight contributions answer to something. Each
// owner says `pkit.Contributes[personaldata.Part]()`, and a contribution nobody
// takes is a claim in a manifest and a dead function in a module: the resolver's own
// explanation prints "no module in platformkit takes one" for exactly that, and for
// eight modules it printed it until this value was composed. `Needs[[]personaldata.Part]`
// is the taking, and it is also the ordering — the resolver builds a consumer after
// every contributor (pkit/declare.go), which is why this module does not say
// After(Everything), which would refuse anything it provides.
//
// It needs the person's own service for one read and no write: an export and an erasure
// both begin with the address and the name, which live in users and nowhere else the
// boundary allows (ADR 0020 §1). It needs the file module and the notification module for
// the door — a bundle's bytes and a person's inbox — and the tenant list for the walk, the
// same lister modules/task's sweep is handed. The two ports in between are declared here,
// in this package, so what the module consumes is two methods and not two services; the
// adapters below are the whole of that wiring, and they are the reason `contracts/` names
// no storage SDK and no mail client.
var Module = pkit.NewModule("subject", wire,
	pkit.Needs[[]personaldata.Part](),
	pkit.Needs[usercontracts.Service](),
	pkit.Needs[filecontracts.Service](),
	pkit.Needs[notificationcontracts.Service](),
	pkit.Needs[jobs.TenantLister](),
	pkit.Provides[subjectcontracts.Service](),
)

func wire(w *pkit.Wiring) (module.Module, error) {
	deps := Deps{
		Parts:   pkit.All[personaldata.Part](w),
		People:  pkit.Get[usercontracts.Service](w),
		Bundles: bundles{files: pkit.Get[filecontracts.Service](w)},
		Notes:   notices{service: pkit.Get[notificationcontracts.Service](w)},
		Tenants: pkit.Get[jobs.TenantLister](w),
		Files:   pkit.Get[filecontracts.Service](w),
	}
	deps.Service = internal.NewService(internal.Deps{
		Parts: deps.Parts, People: deps.People,
		Bundles: deps.Bundles, Notes: deps.Notes,
	})
	pkit.Put[subjectcontracts.Service](w, deps.Service)
	return NewWith(deps), nil
}

// bundles stores a person's bundle through the file module: private, and held against the
// retention sweep for exactly as long as the link to it lives. The hold is not optional —
// a sweep that removed the bytes under an open link would leave a person with a link that
// 404s and no way to ask for the copy they were owed.
type bundles struct{ files filecontracts.Service }

func (b bundles) Put(ctx context.Context, tx subjectcontracts.Tx, name string, body []byte) (uuid.UUID, error) {
	if b.files == nil {
		return uuid.Nil, context.Canceled
	}
	// The bundle is already in memory — nine owners' JSON, made in this transaction — so
	// the reason the file module asks for a lazy transaction (a slow client pinning a
	// connection) does not apply, and the open one is handed back to it.
	open, err := tx(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	f, err := b.files.Upload(ctx, func(context.Context) (db.Tx[db.Tenant], error) { return open, nil },
		filecontracts.Upload{
			// Private, a zip, and no retention class: the class is the product's token to
			// invent (modules/file matches it against the deployment's own table and deletes
			// nothing it does not find there), and a bundle's life is its hold, which the
			// line below places and Mint moves. Naming a class from here would be the
			// module naming a deployment's retention policy, and the reference application
			// answers it with `"subject.export" is not a retention class`.
			Name: name, ContentType: "application/zip", Visibility: filecontracts.VisibilityPrivate,
			Declared: int64(len(body)), Body: bytes.NewReader(body),
		})
	if err != nil {
		return uuid.Nil, err
	}
	return f.ID, nil
}

// Hold places or replaces the retention hold, which is what keeps the sweep from removing
// the bytes while a person's link is still open. The reason is the whole sentence: a bundle
// is the most sensitive object in the store and the only copy of an answer the tenant owes a
// person, and it is also the one object a sweep could delete out from under an open link.
func (b bundles) Hold(ctx context.Context, tx db.Tx[db.Tenant], file uuid.UUID, until time.Time) error {
	if b.files == nil {
		return context.Canceled
	}
	_, err := b.files.Retain(ctx, tx, file, &until,
		"a person's export bundle, held while its download link is open")
	return err
}

// Discard ends a bundle the erasure of its person has overtaken: the hold this module
// placed while a link was open, then the row and the bytes behind it. Release before Delete
// because the file module asks that question before every removal — a hold is a promise to
// somebody that the bytes outlive the class they were filed under, and the erasure is the
// moment that promise is withdrawn.
func (b bundles) Discard(ctx context.Context, tx db.Tx[db.Tenant], file uuid.UUID) error {
	if b.files == nil {
		return context.Canceled
	}
	if err := b.files.Release(ctx, tx, file); err != nil {
		return err
	}
	_, err := b.files.Delete(ctx, tx, file)
	return err
}

// notices tells the person their bundle is ready, through the module that owns inboxes.
// It says the bundle exists and points at the request; the token is never in it, because a
// link in a row is a link a database read hands out (ADR 0020 §6).
type notices struct{ service notificationcontracts.Service }

func (n notices) Ready(ctx context.Context, tx db.Tx[db.Tenant], notice subjectcontracts.Notice) error {
	if n.service == nil {
		return nil
	}
	_, err := n.service.Notify(ctx, tx, notificationcontracts.Notice{
		Recipient: notice.Recipient, Title: notice.Title, Body: notice.Body, Link: notice.Link,
	})
	return err
}
