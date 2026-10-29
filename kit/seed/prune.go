package seed

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
)

// prune visits referring resources before targets and only deletes mappings
// from opted-in documents. Declared canonical keys and resolved row IDs protect
// records moved between kinds or stored under another spelling.
func (s *Service) prune(ctx context.Context, tx db.Tx[db.Tenant], docs []Document, resolved map[string]uuid.UUID, apply bool, plan *Plan) error {
	byAlias, declared, declaredRows := s.pruneScope(docs, resolved)
	if len(byAlias) == 0 {
		return nil
	}
	resources, err := s.pruneOrder(byAlias)
	if err != nil {
		return err
	}
	for i := len(resources) - 1; i >= 0; i-- {
		alias := resources[i]
		writer := s.writers[alias]
		resource := writer.Resource()
		for _, doc := range byAlias[alias] {
			keep := declared[alias]
			keys, err := ownedKeys(tx, resource, doc.Kind)
			if err != nil {
				return fmt.Errorf("seed: %s: %w", doc.Source, err)
			}
			// A prune is a write the file caused, so its event cites the file too.
			dctx := events.WithAttribution(ctx, seedAttribution(ctx, doc.Source))
			target := pruneTarget{writer: writer, resource: resource, doc: doc, rows: declaredRows[alias]}
			for _, key := range keys {
				if keep[key.Value] {
					continue
				}
				item, err := s.pruneCandidate(ctx, dctx, tx, target, key, apply)
				if err != nil {
					return err
				}
				if item != nil {
					plan.Items = append(plan.Items, *item)
				}
			}
		}
	}
	return nil
}

func (s *Service) pruneScope(docs []Document, resolved map[string]uuid.UUID) (map[string][]Document, map[string]map[string]bool, map[string]map[uuid.UUID]bool) {
	byAlias := make(map[string][]Document)
	declared := make(map[string]map[string]bool)
	declaredRows := make(map[string]map[uuid.UUID]bool)
	for _, doc := range docs {
		keys := declared[doc.Resource]
		if keys == nil {
			keys = make(map[string]bool)
			declared[doc.Resource] = keys
			declaredRows[doc.Resource] = make(map[uuid.UUID]bool)
		}
		// The keep-set is written in the identity the owner stores, because that is
		// how the mappings prune reads are written: compare the file's spelling to
		// the owner's and a respelled key keeps no record, it deletes it.
		owner := s.writers[doc.Resource].Resource()
		for _, record := range doc.Records {
			identity := canonicalKey(owner, record.Key)
			keys[identity] = true
			for _, name := range []string{identity, record.Key} {
				if id, known := resolved[doc.Resource+"/"+name]; known && id != uuid.Nil {
					declaredRows[doc.Resource][id] = true
				}
			}
		}
		if doc.Prune {
			byAlias[doc.Resource] = append(byAlias[doc.Resource], doc)
		}
	}
	return byAlias, declared, declaredRows
}

func (s *Service) pruneOrder(byAlias map[string][]Document) ([]string, error) {
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	var resources []string
	var visit func(string) error
	visit = func(alias string) error {
		if visiting[alias] {
			return fmt.Errorf("seed: resource reference cycle involving %s prevents prune", alias)
		}
		if visited[alias] {
			return nil
		}
		visiting[alias] = true
		for _, ref := range s.writers[alias].Resource().References {
			if ref.Target != alias && len(byAlias[ref.Target]) > 0 {
				if err := visit(ref.Target); err != nil {
					return err
				}
			}
		}
		visiting[alias], visited[alias] = false, true
		resources = append(resources, alias)
		return nil
	}
	for _, alias := range slices.Sorted(maps.Keys(byAlias)) {
		if err := visit(alias); err != nil {
			return nil, err
		}
	}
	return resources, nil
}

type pruneTarget struct {
	writer   Writer
	resource Resource
	doc      Document
	rows     map[uuid.UUID]bool
}

func (s *Service) pruneCandidate(ctx, dctx context.Context, tx db.Tx[db.Tenant], target pruneTarget, key Key, apply bool) (*Item, error) {
	if err := s.authorize.Check(ctx, tx, target.resource, Prune); err != nil {
		return nil, fmt.Errorf("seed: %s: %w", target.doc.Source, err)
	}
	row, err := target.writer.Read(dctx, tx, key, apply)
	if err != nil {
		return nil, fmt.Errorf("seed: %s: %w", target.doc.Source, err)
	}
	if row.Present && target.rows[row.ID] {
		// Keep a row this run resolved, even when an older mapping spells its key
		// differently from the owner.
		return nil, nil
	}
	if !row.Present {
		// An absent owner row is already deleted; only its provenance needs removal.
		if apply {
			if err := deleteKey(tx, target.resource, key.Value); err != nil {
				return nil, fmt.Errorf("seed: %s: %w", target.doc.Source, err)
			}
		}
		return &Item{Action: Prune, Resource: target.doc.Resource, Key: key.Value, Source: target.doc.Source}, nil
	}
	if apply {
		if err := target.writer.Delete(dctx, tx, row); err != nil {
			return nil, fmt.Errorf("seed: %s: %w", target.doc.Source, err)
		}
		if err := deleteKey(tx, target.resource, key.Value); err != nil {
			return nil, fmt.Errorf("seed: %s: %w", target.doc.Source, err)
		}
	}
	return &Item{Action: Prune, Resource: target.doc.Resource, Key: key.Value, Source: target.doc.Source, RecordID: row.ID}, nil
}
