package seed

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

func (s *Service) run(ctx context.Context, tx db.Tx[db.Tenant], selection Selection, provisioning, apply bool) (Plan, error) {
	plan, err := s.selectPlan(tx, selection)
	if err != nil {
		return Plan{}, err
	}
	documents, err := Load(s.files, s.root, plan.Kinds...)
	if err != nil {
		return Plan{}, err
	}
	references, err := s.validateDocuments(documents, &plan)
	if err != nil {
		return Plan{}, err
	}
	resolved := make(map[string]uuid.UUID)
	ordered, err := Order(documents, references, func(resource, key string) (bool, error) {
		id, present, err := s.existing(ctx, tx, resource, key)
		if err != nil {
			return false, err
		}
		if present {
			resolved[resource+"/"+key] = id
		}
		return present, nil
	})
	if err != nil {
		return Plan{}, err
	}
	for _, entry := range ordered {
		item, err := s.reconcile(ctx, tx, entry, resolved, plan.At, provisioning, apply)
		if err != nil {
			return Plan{}, err
		}
		plan.Items = append(plan.Items, item)
	}
	if err := s.prune(ctx, tx, documents, resolved, apply, &plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// existing answers the question a reference to a record outside these files
// asks: does this tenant already hold the row it names. It is the same two
// reads a record's own pass makes — the provenance mapping, then the owner's
// row by the ID that mapping holds or, for a resource with a natural key, by
// that key — because a target the seed wrote earlier is often known to the seed
// by nothing but its opaque key.
func (s *Service) existing(ctx context.Context, tx db.Tx[db.Tenant], alias, key string) (uuid.UUID, bool, error) {
	writer := s.writers[alias]
	if writer == nil {
		return uuid.Nil, false, nil
	}
	r := writer.Resource()
	name, _, err := lookupKey(tx, r, canonicalKey(r, key))
	if err != nil {
		return uuid.Nil, false, err
	}
	row, err := writer.Read(ctx, tx, name, false)
	if err != nil {
		return uuid.Nil, false, err
	}
	if !row.Present {
		return uuid.Nil, false, nil
	}
	return row.ID, true, nil
}

func (s *Service) selectPlan(tx db.Tx[db.Tenant], selection Selection) (Plan, error) {
	var plan Plan
	plan.TenantID = db.TenantOf(tx).ID
	plan.At = s.clock.Now().UTC()
	plan.Kinds = []string{"starter"}
	if selection.Demo {
		demo, err := persistedDemo(tx)
		if err != nil {
			return Plan{}, err
		}
		if !demo {
			return Plan{}, errors.New("seed: demo records are refused for a non-demo tenant")
		}
		plan.Kinds = append(plan.Kinds, "demo")
	}
	return plan, nil
}

func (s *Service) validateDocuments(documents []Document, plan *Plan) ([]Reference, error) {
	var references []Reference
	// One record per identity across every file this run loaded, keyed by the
	// identity its owner stores. Order catches two identical keys; this catches
	// the pair that differs only in the spelling the owner folds away.
	spellings := make(map[string]Source)
	// A resource is described once, whatever number of kinds' files declare it: the
	// references below belong to its Writer, and appending them per document would
	// turn one honest declaration into the duplicate Order rightly refuses.
	listed := make(map[string]bool)
	for _, doc := range documents {
		writer := s.writers[doc.Resource]
		if writer == nil {
			return nil, fmt.Errorf("seed: %s: no writer for %s", doc.Source, doc.Resource)
		}
		r := writer.Resource()
		if doc.Prune && !r.Prunable {
			return nil, fmt.Errorf("seed: %s: %s has no owner delete path", doc.Source, doc.Resource)
		}
		for _, record := range doc.Records {
			// Two spellings of one identity are one record declared twice, and the
			// run cannot write a row twice: refuse where the second declaration is
			// written rather than letting one row meet two sets of fields and keep
			// whichever came last.
			identity := canonicalKey(r, record.Key)
			if prior, again := spellings[r.Alias+"/"+identity]; again {
				return nil, fmt.Errorf("seed: %s: %s/%s is the record already declared at %s", record.Source, r.Alias, identity, prior)
			}
			spellings[r.Alias+"/"+identity] = record.Source
			for _, command := range record.Commands {
				if !slices.Contains(r.Commands, command.Name) {
					return nil, fmt.Errorf("seed: %s: %s has no command %s", command.Source, doc.Resource, command.Name)
				}
			}
			if len(record.I18n) > 0 {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s/%s i18n skipped: translation writer unavailable", doc.Resource, record.Key))
			}
		}
		if !listed[r.Alias] {
			listed[r.Alias] = true
			references = append(references, r.References...)
		}
	}
	return references, nil
}
