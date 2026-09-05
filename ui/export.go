package ui

import (
	"cmp"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/icon"
)

// DesignExport is a flat snapshot of already-bound Go examples, not a language
// for constructing pages. An editor adapter consumes it; the application never
// executes it. PropsEditable and slot support describe the Go example API, not
// native editor support, accessibility approval or production readiness.
type DesignExport struct {
	Schema     string                          `json:"schema"`
	SHA256     string                          `json:"sha256,omitempty"`
	FontPolicy string                          `json:"fontPolicy"`
	Notices    string                          `json:"notices"`
	Themes     []ThemeExport                   `json:"themes"`
	Icons      []IconExport                    `json:"icons"`
	Examples   []components.ExampleDescription `json:"examples"`
	CSS        string                          `json:"css"`
}

// ThemeExport separates the CSS selector's stable mode from its display name.
// Font tokens are CSS fallback stacks; this export bundles no font files.
type ThemeExport struct {
	Mode   string         `json:"mode"`
	Name   string         `json:"name"`
	Tokens []design.Token `json:"tokens"`
}

// IconExport carries the exact canonical vector and its attribution. SHA256
// addresses the SVG bytes, not the upstream repository's unpinned contents.
type IconExport struct {
	Name    string `json:"name"`
	SVG     string `json:"svg"`
	Source  string `json:"source"`
	License string `json:"license"`
	SHA256  string `json:"sha256"`
}

// The snapshot redistributes vector bytes, so attribution and license texts
// travel with it. Tests keep this resource synchronized with LICENSE and NOTICE.
//
//go:embed design-notices.txt
var designNotices string

// Export captures one consumer's palette, examples and stylesheet additions.
// Pass components.Gallery() for Core; a product supplies its own explicitly
// bound examples. No registration, network access or files are involved.
//
// Examples are sorted by stable ID without changing the caller's slice. The
// digest covers encoding/json's compact output with SHA256 omitted; it changes
// with the exported content, not timestamps or unrelated repository edits.
func Export(theme design.Pair, examples []components.Example, extra ...Extra) (DesignExport, error) {
	out := DesignExport{
		Schema: "platformkit.design-export.v1", FontPolicy: "system-fallback-stacks", Notices: designNotices,
		Themes: []ThemeExport{
			{Mode: "light", Name: theme.Light.Name, Tokens: theme.Light.Tokens()},
			{Mode: "dark", Name: theme.Dark.Name, Tokens: theme.Dark.Tokens()},
		},
		Icons:    []IconExport{},
		Examples: []components.ExampleDescription{},
	}
	seen := make(map[string]bool, len(examples))
	schemas := make(map[string]string)
	for _, example := range examples {
		if strings.TrimSpace(example.ID) == "" || strings.TrimSpace(example.ComponentID) == "" {
			return DesignExport{}, fmt.Errorf("design export: example %q has no stable identity", example.Name)
		}
		if seen[example.ID] {
			return DesignExport{}, fmt.Errorf("design export: duplicate example ID %q", example.ID)
		}
		seen[example.ID] = true
		description, err := example.Describe()
		if err != nil {
			return DesignExport{}, fmt.Errorf("design export %s: %w", example.ID, err)
		}
		if description.PropsEditable {
			schema := string(description.Schema)
			if previous, exists := schemas[example.ComponentID]; exists && previous != schema {
				return DesignExport{}, fmt.Errorf("design export: conflicting property contracts for %q", example.ComponentID)
			}
			schemas[example.ComponentID] = schema
		}
		out.Examples = append(out.Examples, description)
	}
	slices.SortFunc(out.Examples, func(a, b components.ExampleDescription) int { return cmp.Compare(a.ID, b.ID) })
	for _, name := range icon.Names() {
		glyph, _ := icon.Resolve(name)
		svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="` + icon.ViewBox + `" fill="currentColor">` + glyph.Body + `</svg>`
		out.Icons = append(out.Icons, IconExport{
			Name: name, SVG: svg, Source: glyph.Source, License: glyph.License, SHA256: digest([]byte(svg)),
		})
	}
	// Compose deduplicates these declarations with the shared shell and every
	// consumer addition. The exported sheet can render every gallery example.
	all := append([]Extra{{Lists: components.ClassLists()}}, extra...)
	out.CSS = string(Compose(theme, all...).Body)
	payload, err := json.Marshal(out)
	if err != nil {
		return DesignExport{}, fmt.Errorf("design export: encode snapshot: %w", err)
	}
	out.SHA256 = digest(payload)
	return out, nil
}

func digest(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
