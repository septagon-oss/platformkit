package designconfig

import (
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/septagon-oss/platformkit/design"
)

// ClientDesignFile is the name a client's identity file carries, one per client
// directory: clients/<slug>/design.yaml.
const ClientDesignFile = "design.yaml"

// LoadClientDesign reads one client's design.yaml and returns what it says. The
// file's own slug has to agree with the directory it was read from, because a
// copy of another client's file is exactly the mistake a directory tree invites,
// and the pair a process ends up wearing is not something to discover in an
// incident.
func LoadClientDesign(fsys fs.FS, slug string) (design.Client, error) {
	contents, err := fs.ReadFile(fsys, path.Join(slug, ClientDesignFile))
	if err != nil {
		return design.Client{}, fmt.Errorf("config: read client design: %w", err)
	}
	// knownfields, not plain decoding: a key nobody reads is a key that was
	// written for a reason and is being ignored.
	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	decoder.KnownFields(true)
	var client design.Client
	if err := decoder.Decode(&client); err != nil {
		return design.Client{}, fmt.Errorf("config: decode %s/%s: %w", slug, ClientDesignFile, err)
	}
	if client.Slug != slug {
		return design.Client{}, fmt.Errorf("config: %s/%s names slug %q", slug, ClientDesignFile, client.Slug)
	}
	if err := client.Validate(); err != nil {
		return design.Client{}, err
	}
	return client, nil
}

// LoadClientDesigns reads every client under a directory and resolves each to
// the design.Pair its composition hands ui.Compose. The result is keyed by slug:
// one process holds many clients this way, and nothing here is a singleton a
// second tenant would have to unpick.
//
// Two clients whose generated palettes would be taken for one another are
// refused here rather than rendered side by side: the second one to arrive gets
// the error, and design.Colliding says why. A client whose override drops one of
// its own body roles below the gate is refused too, and nothing partial is
// returned.
func LoadClientDesigns(fsys fs.FS) (map[string]design.Pair, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("config: read client design directory: %w", err)
	}
	pairs := make(map[string]design.Pair, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := fs.Stat(fsys, path.Join(entry.Name(), ClientDesignFile)); err != nil {
			continue
		}
		client, err := LoadClientDesign(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
		pair, err := client.Resolve()
		if err != nil {
			return nil, err
		}
		for _, slug := range slices.Sorted(maps.Keys(pairs)) {
			if design.Colliding(pairs[slug], pair) {
				return nil, fmt.Errorf("config: client %s generates a palette %.3f from client %s, below the %.2f a reader needs to tell them apart",
					client.Slug, design.Distance(pairs[slug], pair), slug, design.MinDistance)
			}
		}
		pairs[client.Slug] = pair
	}
	return pairs, nil
}
