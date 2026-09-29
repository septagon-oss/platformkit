// Package designconfig decodes a client's design.yaml into the identity that
// client's composition wears.
//
// One file per client, clients/<slug>/design.yaml, read through io/fs so a
// composition decides whether that is a disk, an embedded directory or a test
// fixture. A file declares a seed and at most a handful of named token overrides;
// LoadClientDesign turns it into a LoadedClient, and designconfig.Register is the
// only route from there to a design.Pair. That indirection is the package: two
// clients whose generated palettes a reader would take for one another are exactly
// the mistake a directory tree invites, the pair a process ends up wearing is not
// something to discover in an incident, and a rule about a set cannot live on the
// door that reads one file.
package designconfig

import (
	"errors"
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
// incident. The result carries no identity of its own: Register.Add gives it one.
func LoadClientDesign(fsys fs.FS, slug string) (LoadedClient, error) {
	contents, err := fs.ReadFile(fsys, path.Join(slug, ClientDesignFile))
	if err != nil {
		return LoadedClient{}, fmt.Errorf("config: read client design: %w", err)
	}
	// knownfields, not plain decoding: a key nobody reads is a key that was
	// written for a reason and is being ignored.
	decoder := yaml.NewDecoder(strings.NewReader(string(contents)))
	decoder.KnownFields(true)
	var client design.Client
	if err := decoder.Decode(&client); err != nil {
		return LoadedClient{}, fmt.Errorf("config: decode %s/%s: %w", slug, ClientDesignFile, err)
	}
	if client.Slug != slug {
		return LoadedClient{}, fmt.Errorf("config: %s/%s names slug %q", slug, ClientDesignFile, client.Slug)
	}
	if err := client.Validate(); err != nil {
		return LoadedClient{}, err
	}
	return LoadedClient{slug: slug, client: client}, nil
}

// LoadedClient is one client's design.yaml, read and validated, that has no
// identity yet. It keeps the declaration behind unexported fields on purpose:
// the only way from here to a design.Pair is Register.Add, so a process cannot
// accumulate clients by reading one directory at a time and end up wearing two
// palettes a reader would take for one another. An identity is declared, but it
// is *worn* as a set, and the set is where the collision rule lives.
type LoadedClient struct {
	slug   string
	client design.Client
}

// Slug is the directory this client was read from, which LoadClientDesign has
// already made agree with the file's own slug.
func (loaded LoadedClient) Slug() string { return loaded.slug }

// Register holds the client identities one process wears. It is the only route
// from a client's file to the pair its composition hands ui.Compose, and it owns
// the one rule a single client cannot state about itself: two clients whose
// generated palettes a reader would take for one another are refused here, the
// second one to arrive gets the error, and design.Colliding says why.
//
// Nothing about a Register is a singleton: one per tenant, one per process, is
// the same value somebody constructed.
type Register struct {
	pairs map[string]design.Pair
}

// NewRegister returns an empty set of client identities.
func NewRegister() *Register { return &Register{pairs: make(map[string]design.Pair)} }

// Add resolves one loaded client and refuses it if any client already in the
// register generates a palette it cannot be told apart from. A refusal adds
// nothing: the register holds exactly what it held before.
func (r *Register) Add(loaded LoadedClient) error {
	pair, err := loaded.client.Resolve()
	if err != nil {
		return err
	}
	for _, slug := range slices.Sorted(maps.Keys(r.pairs)) {
		if design.Colliding(r.pairs[slug], pair) {
			return fmt.Errorf("config: client %s generates a palette %.3f from client %s, below the %.2f a reader needs to tell them apart",
				loaded.slug, design.Distance(r.pairs[slug], pair), slug, design.MinDistance)
		}
	}
	if r.pairs == nil {
		r.pairs = make(map[string]design.Pair)
	}
	r.pairs[loaded.slug] = pair
	return nil
}

// Pair returns the identity of one registered client.
func (r *Register) Pair(slug string) (design.Pair, bool) {
	pair, ok := r.pairs[slug]
	return pair, ok
}

// Pairs is the whole set, keyed by slug, detached from the register.
func (r *Register) Pairs() map[string]design.Pair {
	return maps.Clone(r.pairs)
}

// LoadClientDesigns reads every client under a directory into a Register and
// returns the identities its composition hands ui.Compose. The result is keyed by
// slug: one process holds many clients this way, and nothing here is a singleton a
// second tenant would have to unpick.
//
// The refusals are the Register's — a second client wearing a palette a reader
// would take for another client's, and a client whose own override drops one of
// its body roles below the gate. Nothing partial is returned: a set that reports
// no error is the complete set of directories on disk, including a directory whose
// file could not be read, which fails the load rather than going unmentioned.
func LoadClientDesigns(fsys fs.FS) (map[string]design.Pair, error) {
	register := NewRegister()
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("config: read client design directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		client, err := LoadClientDesign(fsys, entry.Name())
		if err != nil {
			// A directory with no design.yaml is not a client; one whose file the
			// filesystem refuses to hand over is a client this process cannot wear,
			// and saying so is the whole reason the loader exists. Silently dropping
			// it would hand that client's screens design.Default() — the quiet
			// substitution the package doc refuses to accept.
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			return nil, err
		}
		if err := register.Add(client); err != nil {
			return nil, err
		}
	}
	return register.Pairs(), nil
}
