package seed

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Reference declares a field or command argument whose value is a
// resource/key. Only declared paths are interpreted as references.
type Reference struct {
	Resource, Path, Target string
}

// Entry retains the kind and owning resource beside an ordered record.
type Entry struct {
	Kind, Resource string
	Record         Record
}

// Order validates declared references and sorts records by dependency. The
// exists callback checks a target that is not in these files in the same
// tenant; nil means such targets are unavailable. It must not cross tenants.
func Order(documents []Document, references []Reference, exists func(resource, key string) (bool, error)) ([]Entry, error) {
	entries := make(map[string]Entry)
	paths := make(map[string]bool)
	for _, ref := range references {
		parts := strings.Split(ref.Path, "/")
		validPath := len(parts) == 2 && parts[0] == "fields" && parts[1] != "" ||
			len(parts) == 3 && parts[0] == "commands" && parts[1] != "" && parts[2] != ""
		if !validName(ref.Resource) || !validName(ref.Target) || !validPath {
			return nil, fmt.Errorf("seed: invalid reference declaration %+v", ref)
		}
		identity := ref.Resource + "/" + ref.Path
		if paths[identity] {
			return nil, fmt.Errorf("seed: duplicate reference declaration %s", identity)
		}
		paths[identity] = true
	}
	references = slices.Clone(references)
	slices.SortFunc(references, func(a, b Reference) int {
		return cmp.Or(cmp.Compare(a.Resource, b.Resource), cmp.Compare(a.Path, b.Path))
	})
	for _, doc := range documents {
		for _, record := range doc.Records {
			identity := doc.Resource + "/" + record.Key
			if prior, duplicate := entries[identity]; duplicate {
				return nil, fmt.Errorf("seed: %s duplicates %s at %s", record.Source, identity, prior.Record.Source)
			}
			entries[identity] = Entry{Kind: doc.Kind, Resource: doc.Resource, Record: record}
		}
	}
	dependents := make(map[string][]string)
	degree := make(map[string]int, len(entries))
	for identity := range entries {
		degree[identity] = 0
	}
	for identity, entry := range entries {
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
				return nil, fmt.Errorf("seed: %s: reference %s must be a string", valueSource(entry.Record, ref.Path), ref.Path)
			}
			alias, targetKey, found := strings.Cut(name, "/")
			if !found || alias != ref.Target || targetKey == "" {
				return nil, fmt.Errorf("seed: %s: reference %q must name %s/<key>", valueSource(entry.Record, ref.Path), name, ref.Target)
			}
			target := alias + "/" + targetKey
			if _, local := entries[target]; !local {
				found := false
				if exists != nil {
					var err error
					found, err = exists(alias, targetKey)
					if err != nil {
						return nil, fmt.Errorf("seed: %s: resolve %s: %w", valueSource(entry.Record, ref.Path), target, err)
					}
				}
				if !found {
					return nil, fmt.Errorf("seed: %s: %s references missing %s", valueSource(entry.Record, ref.Path), identity, target)
				}
				continue
			}
			dependents[target] = append(dependents[target], identity)
			degree[identity]++
		}
	}
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
	if len(ordered) != len(entries) {
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
				return nil, fmt.Errorf("seed: %s: reference from %s to %s at %s closes a cycle", valueSource(entries[first].Record, ref.Path), first, name, entries[name].Record.Source)
			}
		}
		return nil, fmt.Errorf("seed: %s: reference cycle includes %s", entries[first].Record.Source, first)
	}
	return ordered, nil
}

func valueAt(record Record, path string) (any, bool) {
	parts := strings.Split(path, "/")
	if len(parts) == 2 && parts[0] == "fields" {
		value, ok := record.Fields[parts[1]]
		return value, ok
	}
	if len(parts) == 3 && parts[0] == "commands" {
		for _, command := range record.Commands {
			if command.Name == parts[1] {
				value, ok := command.Args[parts[2]]
				return value, ok
			}
		}
	}
	return nil, false
}

func valueSource(record Record, path string) Source {
	if at, ok := record.Values[path]; ok {
		return at
	}
	return record.Source
}
