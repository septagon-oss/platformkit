package seed

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
)

func validateReferences(references []Reference) error {
	paths := make(map[string]bool)
	for _, ref := range references {
		parts := strings.Split(ref.Path, "/")
		validPath := len(parts) == 2 && parts[0] == "fields" && parts[1] != "" ||
			len(parts) == 3 && parts[0] == "commands" && parts[1] != "" && parts[2] != ""
		if !validName(ref.Resource) || !validName(ref.Target) || !validPath {
			return fmt.Errorf("seed: invalid reference declaration %+v", ref)
		}
		identity := ref.Resource + "/" + ref.Path
		if paths[identity] {
			return fmt.Errorf("seed: duplicate reference declaration %s", identity)
		}
		paths[identity] = true
	}
	return nil
}

func indexEntries(documents []Document) (map[string]Entry, error) {
	entries := make(map[string]Entry)
	for _, doc := range documents {
		for _, record := range doc.Records {
			identity := doc.Resource + "/" + record.Key
			if prior, duplicate := entries[identity]; duplicate {
				return nil, fmt.Errorf("seed: %s duplicates %s at %s", record.Source, identity, prior.Record.Source)
			}
			entries[identity] = Entry{Kind: doc.Kind, Resource: doc.Resource, Record: record}
		}
	}
	return entries, nil
}

func dependencyGraph(entries map[string]Entry, references []Reference, exists func(resource, key string) (bool, error)) (map[string][]string, map[string]int, error) {
	dependents := make(map[string][]string)
	degree := make(map[string]int, len(entries))
	for identity := range entries {
		degree[identity] = 0
	}
	for _, identity := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[identity]
		for _, ref := range references {
			if ref.Resource != entry.Resource {
				continue
			}
			value, present := valueAt(entry.Record, ref.Path)
			if !present {
				continue
			}
			name, ok := value.(string)
			if !ok {
				return nil, nil, fmt.Errorf("seed: %s: reference %s must be a string", valueSource(entry.Record, ref.Path), ref.Path)
			}
			alias, targetKey, found := strings.Cut(name, "/")
			if !found || alias != ref.Target || targetKey == "" {
				return nil, nil, fmt.Errorf("seed: %s: reference %q must name %s/<key>", valueSource(entry.Record, ref.Path), name, ref.Target)
			}
			target := alias + "/" + targetKey
			if _, local := entries[target]; !local {
				found := false
				if exists != nil {
					var err error
					found, err = exists(alias, targetKey)
					if err != nil {
						return nil, nil, fmt.Errorf("seed: %s: resolve %s: %w", valueSource(entry.Record, ref.Path), target, err)
					}
				}
				if !found {
					return nil, nil, fmt.Errorf("seed: %s: %s references missing %s", valueSource(entry.Record, ref.Path), identity, target)
				}
				continue
			}
			dependents[target] = append(dependents[target], identity)
			degree[identity]++
		}
	}
	return dependents, degree, nil
}

func orderEntries(entries map[string]Entry, dependents map[string][]string, degree map[string]int) []Entry {
	ready := make([]string, 0, len(entries))
	for identity, count := range degree {
		if count == 0 {
			ready = append(ready, identity)
		}
	}
	slices.Sort(ready)
	ordered := make([]Entry, 0, len(entries))
	for len(ready) > 0 {
		identity := ready[0]
		ready = ready[1:]
		ordered = append(ordered, entries[identity])
		for _, dependent := range dependents[identity] {
			degree[dependent]--
			if degree[dependent] == 0 {
				ready = append(ready, dependent)
			}
		}
		slices.Sort(ready)
	}
	return ordered
}

func referenceCycle(entries map[string]Entry, references []Reference, degree map[string]int) error {
	remaining := make([]string, 0)
	for identity, count := range degree {
		if count > 0 {
			remaining = append(remaining, identity)
		}
	}
	slices.SortFunc(remaining, cmp.Compare[string])
	first := remaining[0]
	for _, ref := range references {
		if ref.Resource != entries[first].Resource {
			continue
		}
		value, ok := valueAt(entries[first].Record, ref.Path)
		if !ok {
			continue
		}
		name, ok := value.(string)
		if ok && degree[name] > 0 {
			return fmt.Errorf("seed: %s: reference from %s to %s at %s closes a cycle", valueSource(entries[first].Record, ref.Path), first, name, entries[name].Record.Source)
		}
	}
	return fmt.Errorf("seed: %s: reference cycle includes %s", entries[first].Record.Source, first)
}
