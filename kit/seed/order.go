package seed

import (
	"cmp"
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
	if err := validateReferences(references); err != nil {
		return nil, err
	}
	references = slices.Clone(references)
	slices.SortFunc(references, func(a, b Reference) int {
		return cmp.Or(cmp.Compare(a.Resource, b.Resource), cmp.Compare(a.Path, b.Path))
	})
	entries, err := indexEntries(documents)
	if err != nil {
		return nil, err
	}
	dependents, degree, err := dependencyGraph(entries, references, exists)
	if err != nil {
		return nil, err
	}
	ordered := orderEntries(entries, dependents, degree)
	if len(ordered) != len(entries) {
		return nil, referenceCycle(entries, references, degree)
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
