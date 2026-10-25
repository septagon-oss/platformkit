package seed

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

func (s *Service) reconcile(ctx context.Context, tx db.Tx[db.Tenant], entry Entry, resolved map[string]uuid.UUID, at time.Time, provisioning, apply bool) (Item, error) {
	writer := s.writers[entry.Resource]
	r := writer.Resource()
	// Attribute owner outbox events to the record that requested the write.
	wctx := events.WithAttribution(ctx, seedAttribution(ctx, entry.Record.Source))
	// Check the update grant before taking an owner row lock.
	if err := s.authorize.Check(ctx, tx, r, Update); err != nil {
		return Item{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
	}
	// Provenance uses the canonical identity; the displayed item keeps the file spelling.
	identity := canonicalKey(r, entry.Record.Key)
	key, owned, err := lookupKey(tx, r, identity)
	if err != nil {
		return Item{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
	}
	current, err := writer.Read(ctx, tx, key, apply)
	if err != nil {
		return Item{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
	}
	target, err := writer.Target(ctx, entry.Record, maps.Clone(resolved), at)
	if err != nil {
		return Item{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
	}
	// Nothing is written for a declaration this run cannot honour: the field is
	// refused at its own line before the row, the mapping and the event.
	if name, where := unappliedField(entry.Record, target); name != "" {
		return Item{}, fmt.Errorf("seed: %s: %s/%s declares %s, which its writer applies nowhere",
			where, entry.Resource, entry.Record.Key, name)
	}
	decision := Decide(current, target)
	if provisioning && decision.Action != Create {
		return Item{}, fmt.Errorf("seed: %s: %s/%s already exists, so this tenant is not being provisioned",
			entry.Record.Source, entry.Resource, entry.Record.Key)
	}
	if current.Present && !owned && decision.Action == Update {
		return Item{}, fmt.Errorf("seed: %s: %s/%s is an unowned natural-key row", entry.Record.Source, entry.Resource, entry.Record.Key)
	}
	// Plan and Apply require the same create grant before reporting a create.
	if decision.Action == Create {
		if err := s.authorize.Check(ctx, tx, r, Create); err != nil {
			return Item{}, fmt.Errorf("seed: %s: %w", entry.Record.Source, err)
		}
	}
	item := Item{Action: decision.Action, Resource: entry.Resource, Key: entry.Record.Key, Source: entry.Record.Source, Changed: decision.Changed, RecordID: current.ID}
	if apply {
		change := reconciliation{writer: writer, resource: r, identity: identity,
			key: key, owned: owned, entry: entry, current: current, target: target, decision: decision}
		current, err = change.apply(wctx, tx)
		if err != nil {
			return Item{}, err
		}
		if decision.Action != Unchanged {
			item.RecordID = current.ID
		}
	}
	// A reference names its target as the file spells it, so it resolves under
	// that spelling; it resolves under the owner's spelling too, which is what
	// lets two files agree on one record without agreeing on its key.
	resolved[entry.Resource+"/"+identity] = current.ID
	resolved[entry.Resource+"/"+entry.Record.Key] = current.ID
	return item, nil
}

type reconciliation struct {
	writer   Writer
	resource Resource
	identity string
	key      Key
	owned    bool
	entry    Entry
	current  Snapshot
	target   Target
	decision Decision
}

func (change reconciliation) apply(wctx context.Context, tx db.Tx[db.Tenant]) (Snapshot, error) {
	current := change.current
	var err error
	switch change.decision.Action {
	case Create:
		// The create grant was checked with the decision, as it is for Plan.
		current, err = change.writer.Create(wctx, tx, change.target)
	case Update:
		current, err = change.writer.Update(wctx, tx, current, change.target)
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("seed: %s: %w", change.entry.Record.Source, err)
	}
	if change.decision.Action != Unchanged {
		if !current.Present {
			return Snapshot{}, fmt.Errorf("seed: %s: owner returned no row after %s", change.entry.Record.Source, change.decision.Action)
		}
		if err := putKey(tx, change.resource, change.identity, change.entry.Kind, current.ID); err != nil {
			return Snapshot{}, fmt.Errorf("seed: %s: %w", change.entry.Record.Source, err)
		}
	} else if change.owned && change.key.Kind != change.entry.Kind {
		// Move provenance with its declaration so a later prune of the old kind
		// cannot delete the unchanged owner row.
		if err := putKey(tx, change.resource, change.identity, change.entry.Kind, current.ID); err != nil {
			return Snapshot{}, fmt.Errorf("seed: %s: %w", change.entry.Record.Source, err)
		}
	}
	return current, nil
}
