package main

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"
)

// moduleLayers is the platform's one module order, lowest rank first, so a layer
// name means the same thing in every repository that runs this gate: rank is
// position here, while each repository's scripts/imports-layers.txt only assigns
// its own modules to a name. A fifth name is a change to this list and a
// dependency repin, never a local order.
//
// platform needs no account, no role and no person; identity owns the account,
// the tenant record, a person's identity, or the channel the platform reaches a
// person through; generic is a reusable capability composed over those two;
// product's reason is an application-facing surface. The full placement rule, and
// the disclaimer that this is neither ADR 0016's acceptance levels nor the UI
// cascade layers, are in the package README.
var moduleLayers = []string{"platform", "identity", "generic", "product"}

// layerUnassigned reports the state of a module the build holds and the
// declaration does not.
const layerUnassigned = "unassigned"

// layerFileSuffix locates one repository's declaration inside its own tree.
const layerFileSuffix = "/scripts/imports-layers.txt"

// The three directions an aggregate edge can run, computed from two ranks and
// never declared.
const (
	directionLateral = "lateral"
	directionDown    = "down"
	directionUpward  = "upward"
)

// layerAssignment is one module's row in the declaration of the repository that
// contains it. The reason a module sits where it sits is the order's placement
// rule plus the measured graph, both of which a reviewer reads, so no row states
// one.
type layerAssignment struct{ owner, layer string }

// layerPair names two modules in sorted order, so one row serves one pair.
type layerPair struct{ a, b string }

func newLayerPair(one, two string) layerPair {
	if two < one {
		one, two = two, one
	}
	return layerPair{one, two}
}

// layerJustification is the only exception a declaration may carry: a reciprocal
// production pair between two modules, with the reason both directions stay. It
// never admits an upward import. judge names the repository whose gate measures
// the pair, empty when this build's own repository carries the row: Go reports
// packages for the main module alone, so a row read out of a dependency's file is
// a fact about that repository, which only its own run can check.
type layerJustification struct {
	pair   layerPair
	reason string
	judge  string
}

// layerDeclaration is everything the readable declarations of one build say:
// assignments keyed by module owner, justifications keyed by module pair. It is
// read-only once loaded.
type layerDeclaration struct {
	assigned       map[string]*layerAssignment
	justifications map[layerPair]*layerJustification
}

func newLayerDeclaration() *layerDeclaration {
	return &layerDeclaration{assigned: map[string]*layerAssignment{}, justifications: map[layerPair]*layerJustification{}}
}

// layer returns a module's layer, or layerUnassigned when it holds no row.
func (d *layerDeclaration) layer(owner string) string {
	if d.assigned[owner] == nil {
		return layerUnassigned
	}
	return d.assigned[owner].layer
}

// rank reports a layer's position in the platform's order.
func rank(layer string) (int, bool) {
	position := slices.Index(moduleLayers, layer)
	return position, position >= 0
}

// direction compares two layers. An unassigned module has no rank to compare,
// and module-layer-missing already names it.
func direction(from, to string) string {
	source, sourceKnown := rank(from)
	target, targetKnown := rank(to)
	if !sourceKnown || !targetKnown {
		return layerUnassigned
	}
	switch {
	case source < target:
		return directionUpward
	case source > target:
		return directionDown
	default:
		return directionLateral
	}
}

// readLayers loads the declaration of the checked repository and of every
// declared dependency root. A missing file declares nothing, which leaves each
// module of that repository unassigned rather than exempt: a map that silently
// absented itself cannot pass. An unreadable or malformed file, and a dependency
// whose tree holds no readable directory in this build context, are errors, so a
// half-read declaration is never reported as a verdict.
func readLayers(g *graph) (*layerDeclaration, error) {
	vendored := false
	if _, err := os.Stat(filepath.Join(g.dir, "vendor", "modules.txt")); err == nil {
		vendored = true
	}
	declared := newLayerDeclaration()
	for _, root := range g.roots {
		if root.dir == "" {
			return nil, fmt.Errorf("%s: repository root %s reports no directory, so its module layer declaration cannot be read", g.dir, root.path)
		}
		if vendored && root.path != g.self {
			return nil, fmt.Errorf("%s: the module layer declaration of %s cannot be read in a vendored build; run the gate with readonly module resolution", root.dir, root.path)
		}
		file := filepath.Join(root.dir, "scripts", "imports-layers.txt")
		data, err := os.ReadFile(file)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := parseLayers(file, string(data), g, root.path, declared); err != nil {
			return nil, err
		}
	}
	return declared, nil
}

// parseLayers adds one repository's rows. Each file may assign only modules of
// the repository that owns it, so the kernel's map travels with the kernel and no
// second copy of one module's layer can drift.
func parseLayers(file, text string, g *graph, rootPath string, into *layerDeclaration) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("%s: module layer rows must be UTF-8", file)
	}
	for i, row := range strings.Split(text, "\n") {
		row = strings.TrimSpace(row)
		if row == "" || strings.HasPrefix(row, "#") {
			continue
		}
		where := fmt.Sprintf("%s:%d", file, i+1)
		fields := strings.Fields(row)
		if len(fields) >= 2 && fields[1] == "<->" {
			pair, reason, err := parseLayerPair(row, fields, g, rootPath, file)
			if err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			if into.justifications[pair] != nil {
				return fmt.Errorf("%s: duplicate pair %s <-> %s", where, pair.a, pair.b)
			}
			judge := ""
			if rootPath != g.self {
				judge = rootPath
			}
			into.justifications[pair] = &layerJustification{pair, reason, judge}
			continue
		}
		owner, layer, err := parseLayerAssignment(fields, g, rootPath, file)
		if err != nil {
			return fmt.Errorf("%s: %w", where, err)
		}
		if into.assigned[owner] != nil {
			return fmt.Errorf("%s: duplicate module %s", where, owner)
		}
		into.assigned[owner] = &layerAssignment{owner, layer}
	}
	return nil
}

// parseLayerAssignment reads `<owner> <layer>`: one full module owner path of
// this repository and one layer name the gate knows. A subpackage, a bare module
// name, traversal, a path of another repository, an unknown name or a third field
// is refused, so the shape of a justification row is never guessed.
func parseLayerAssignment(fields []string, g *graph, rootPath, file string) (string, string, error) {
	if len(fields) != 2 {
		return "", "", fmt.Errorf("expected one module owner and one layer name in %s", file)
	}
	owner, err := g.module(fields[0], rootPath, file)
	if err != nil {
		return "", "", err
	}
	if !slices.Contains(moduleLayers, fields[1]) {
		return "", "", fmt.Errorf("layer %q is not one the gate knows: %s", fields[1], strings.Join(moduleLayers, ", "))
	}
	return owner, fields[1], nil
}

// parseLayerPair reads `<owner> <-> <owner> <reason>`. Both sides must be modules
// of the repository whose file this is: no build measures a pair that spans two
// repositories, because Go reports packages for the main module alone, so neither
// side's gate could judge such a row and no repository owns it. The reason keeps
// its own wording because this row is the exception, and an exception nobody can
// read is not reviewable.
func parseLayerPair(row string, fields []string, g *graph, rootPath, file string) (layerPair, string, error) {
	if len(fields) < 4 {
		return layerPair{}, "", fmt.Errorf("expected two module owners and a nonempty reason in %s", file)
	}
	one, err := g.module(fields[0], "", file)
	if err != nil {
		return layerPair{}, "", err
	}
	two, err := g.module(fields[2], "", file)
	if err != nil {
		return layerPair{}, "", err
	}
	if one == two {
		return layerPair{}, "", fmt.Errorf("pair %s names one module twice", one)
	}
	if g.root(one) != rootPath || g.root(two) != rootPath {
		return layerPair{}, "", fmt.Errorf("pair %s <-> %s names a module outside %s; a pair is justifiable only in the repository that holds both of its modules", one, two, rootPath)
	}
	reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(row, fields[0])), "<->")), fields[2]))
	if reason == "" {
		return layerPair{}, "", fmt.Errorf("pair %s <-> %s needs the reason both directions stay", one, two)
	}
	return newLayerPair(one, two), reason, nil
}

// module validates one declared owner path. With rootPath non-empty, the module
// must belong to that repository.
func (g *graph) module(name, rootPath, file string) (string, error) {
	if !cleanImport(name) {
		return "", fmt.Errorf("invalid module owner %q", name)
	}
	owner, rest, unknown := g.owner(name)
	if owner == "" || rest != "" || unknown || owner != name {
		return "", fmt.Errorf("%q must name one module of a declared repository root in %s", name, file)
	}
	root := g.root(owner)
	short, ok := strings.CutPrefix(owner, root+"/modules/")
	if !ok || short == "" || !cleanImport(short) {
		return "", fmt.Errorf("%q names no module of a declared repository root", name)
	}
	if rootPath != "" && root != rootPath {
		return "", fmt.Errorf("%s assigns a module of %s; a file assigns only modules its own repository contains", file, root)
	}
	return owner, nil
}

// notices announces every assignment once with the state this build gives it, and
// every justification once with its complete reason and with which build measures
// the pair, in the shape the integration and contract-baseline notices use. A row
// this build cannot measure is announced as that other repository's own verdict
// rather than as a pair that failed to reciprocate here.
func (d *layerDeclaration) notices(spent map[string]bool, justified map[layerPair]bool) string {
	var text strings.Builder
	for _, owner := range slices.Sorted(maps.Keys(d.assigned)) {
		state := "holds no package in this build context"
		if spent[owner] {
			state = "assigned in this build context"
		}
		fmt.Fprintf(&text, "imports: module layer %s is %s; %s\n", owner, d.assigned[owner].layer, state)
	}
	for _, pair := range slices.SortedFunc(maps.Keys(d.justifications), func(a, b layerPair) int {
		return cmp.Or(cmp.Compare(a.a, b.a), cmp.Compare(a.b, b.b))
	}) {
		row := d.justifications[pair]
		state := "reciprocal in production in this build context"
		switch {
		case row.judge != "":
			// Naming the owner keeps a reader of a consumer's output from deleting a
			// dependency's row as stale: this build cannot see the pair at all.
			state = "not measured in this build context; " + row.judge + " judges this row in its own build"
		case !justified[pair]:
			state = "not reciprocal in production in this build context"
		}
		fmt.Fprintf(&text, "imports: module layer pair %s <-> %s; %s; %s\n", pair.a, pair.b, state, d.justifications[pair].reason)
	}
	return text.String()
}
