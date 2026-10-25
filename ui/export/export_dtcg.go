package export

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui/style"
)

var ErrDTCGUnsupported = errors.New("selection is not representable as DTCG 2025.10")

// DTCGDiagnostic addresses the source identity, not an array index or encoded
// interchange name. Unsupported means no document can be returned. Other
// diagnostics describe declaration meaning retained only in source metadata.
type DTCGDiagnostic struct {
	Path        []string `json:"path"`
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Unsupported bool     `json:"unsupported,omitzero"`
}

const dtcgExtension = "dev.septagon.platformkit"

// DTCG projects one explicitly selected mode of a validated TokenExport into
// the documented DTCG 2025.10 subset. Use an empty mode only for mode-independent
// selections. Output is deterministic and detached; no files or registries are
// read or written. Any unsupported value returns nil bytes plus all projection
// diagnostics. Successful output embeds its diagnostics and source identities;
// it does not promise editable derivations, imported fonts or a round trip.
func (s TokenExport) DTCG(mode string) ([]byte, []DTCGDiagnostic, error) {
	if err := s.Validate(); err != nil {
		return nil, nil, fmt.Errorf("DTCG source: %w", err)
	}
	var selected TokenMode
	if len(s.Modes) != 0 {
		index := slices.IndexFunc(s.Modes, func(m TokenMode) bool { return m.Mode == mode })
		if index < 0 {
			return nil, nil, fmt.Errorf("DTCG: mode %q must be explicitly present in the selection", mode)
		}
		selected = s.Modes[index]
	} else if mode != "" {
		return nil, nil, fmt.Errorf("DTCG: mode %q is not selected", mode)
	}
	p := dtcgProjection{document: map[string]any{}, diagnostics: []DTCGDiagnostic{}}
	if err := p.colors(s, selected, mode); err != nil {
		return nil, nil, err
	}
	p.fonts(selected, mode)
	p.dimensions(selected, mode)
	p.scales(s)
	p.easings(s)
	p.shadows(s)
	p.evidence(s)
	return p.finish(s, mode)
}

type dtcgProjection struct {
	document    map[string]any
	diagnostics []DTCGDiagnostic
	unsupported bool
}

func (p *dtcgProjection) report(path []string, code, message string, blocked bool) {
	p.diagnostics = append(p.diagnostics, DTCGDiagnostic{slices.Clone(path), code, message, blocked})
	p.unsupported = p.unsupported || blocked
}

func (p *dtcgProjection) emit(path, sourcePath []string, kind string, value, source any) {
	group := p.document
	for _, segment := range path[:len(path)-1] {
		name := dtcgName(segment)
		if group[name] == nil {
			group[name] = map[string]any{}
		}
		group = group[name].(map[string]any)
	}
	group[dtcgName(path[len(path)-1])] = map[string]any{
		"$type": kind, "$value": value,
		"$extensions": map[string]any{dtcgExtension: map[string]any{"sourcePath": sourcePath, "source": source}},
	}
}

func (p *dtcgProjection) colors(s TokenExport, selected TokenMode, mode string) error {
	resolved, err := design.ResolveColors(selected.Colors, s.Colors)
	if err != nil {
		return err
	}
	for _, color := range selected.Colors {
		p.emit([]string{"colors", color.Name}, []string{"modes", mode, "colors", color.Name}, "color", dtcgColor(resolved[color.Name]), color)
	}
	for _, color := range s.Colors {
		path := []string{"colors", color.Name}
		var value any = dtcgColor(resolved[color.Name])
		if color.Value.Reference != "" {
			value = "{colors." + dtcgName(color.Value.Reference) + "}"
		}
		if color.Value.Mix != nil {
			p.report(path, "resolved-color-mix", "DTCG receives the resolved mode colour; editable mix operands remain source metadata", false)
		}
		p.emit(path, path, "color", value, color)
	}
	return nil
}

func (p *dtcgProjection) fonts(selected TokenMode, mode string) {
	for _, font := range selected.Fonts {
		path := []string{"modes", mode, "fonts", font.Name}
		names := make([]string, 0, len(font.Families))
		ambiguous := false
		for _, family := range font.Families {
			names = append(names, family.Name)
			ambiguous = ambiguous || (strings.HasPrefix(family.Name, "{") && strings.HasSuffix(family.Name, "}"))
		}
		if ambiguous {
			p.report(path, "font-family-reference", "literal family resembles a DTCG reference and cannot be escaped without changing its name", true)
			continue
		}
		p.report(path, "font-family-kinds", "DTCG retains ordered names but has no literal-versus-generic discriminator; typed kinds remain source metadata", false)
		p.emit([]string{"fonts", font.Name}, path, "fontFamily", names, font)
	}
}

func (p *dtcgProjection) dimensions(selected TokenMode, mode string) {
	for _, dimension := range selected.Dimensions {
		path := []string{"modes", mode, "shape", dimension.Name}
		// Validate already refuses a unit this format cannot carry; the emitter
		// still says so rather than writing a document with half a shape in it.
		number, unit, err := design.ParseRadius(dimension.Value)
		if err != nil {
			p.report(path, "unsupported-shape-unit", "a DTCG dimension requires px or rem; no conversion was attempted", true)
			continue
		}
		p.emit([]string{"shape", dimension.Name}, path, "dimension", style.Scalar{Value: json.Number(number), Unit: unit}, dimension)
	}
}

func (p *dtcgProjection) scales(s TokenExport) {
	for _, scale := range s.Scales {
		path := []string{"scales", scale.Scale, scale.Key}
		if scale.Number == nil || !slices.Contains([]string{"", "px", "rem", "ms", "s"}, scale.Number.Unit) {
			p.report(path, "unsupported-scale", "keyword or contextual unit has no corresponding DTCG value; no pixel conversion was attempted", true)
			continue
		}
		kind, value := "dimension", any(*scale.Number)
		switch {
		case scale.Scale == "font-weight":
			kind, value = "fontWeight", scale.Number.Value
		case scale.Number.Unit == "":
			kind, value = "number", scale.Number.Value
		case scale.Scale == "duration":
			kind = "duration"
		}
		p.emit(path, path, kind, value, scale)
	}
}

func (p *dtcgProjection) easings(s TokenExport) {
	for _, easing := range s.Easings {
		path := []string{"easings", easing.Key}
		points := [4]float64{0, 0, 1, 1}
		if easing.CubicBezier != nil {
			points = *easing.CubicBezier
		} else {
			p.report(path, "linear-keyword", "linear becomes its equivalent cubic curve; the keyword declaration remains source metadata", false)
		}
		p.emit(path, path, "cubicBezier", points, easing)
	}
}

func (p *dtcgProjection) shadows(s TokenExport) {
	for _, shadow := range s.Shadows {
		path := []string{"shadows", shadow.Key}
		layers := make([]map[string]any, 0, len(shadow.Layers))
		omitted, contextual := false, false
		for _, layer := range shadow.Layers {
			for _, length := range []*style.Scalar{&layer.Blur, &layer.Spread} {
				if *length == (style.Scalar{}) {
					omitted = true
					*length = style.Scalar{Value: "0", Unit: "px"}
				}
			}
			for _, length := range []style.Scalar{layer.OffsetX, layer.OffsetY, layer.Blur, layer.Spread} {
				contextual = contextual || (length.Unit != "px" && length.Unit != "rem")
			}
			layers = append(layers, map[string]any{"offsetX": layer.OffsetX, "offsetY": layer.OffsetY,
				"blur": layer.Blur, "spread": layer.Spread, "color": dtcgColor(layer.Color), "inset": layer.Inset})
		}
		if omitted {
			p.report(path, "shadow-default-length", "omitted shadow blur/spread become explicit zero px; omitted declarations remain source metadata", false)
		}
		if contextual {
			p.report(path, "unsupported-shadow-unit", "DTCG shadow lengths require px/rem; contextual units were not converted", true)
			continue
		}
		p.emit(path, path, "shadow", layers, shadow)
	}
}

func (p *dtcgProjection) evidence(s TokenExport) {
	for _, transition := range s.Transitions {
		p.report([]string{"transitions", transition.Key}, "transition-declarations", "source property groups and absent delay declarations have no complete DTCG transition representation", true)
	}
	for _, asset := range s.Assets {
		p.report([]string{"assets", asset.ID}, "asset-metadata", "asset evidence is preserved as metadata, not a DTCG file token or bundled bytes", false)
	}
	for _, face := range s.Faces {
		p.report([]string{"faces", face.ID}, "face-metadata", "face evidence is preserved as metadata, not an installed or interchangeable font face", false)
	}
}

func (p *dtcgProjection) finish(s TokenExport, mode string) ([]byte, []DTCGDiagnostic, error) {
	slices.SortFunc(p.diagnostics, func(a, b DTCGDiagnostic) int {
		return cmp.Or(slices.Compare(a.Path, b.Path), strings.Compare(a.Code, b.Code))
	})
	if p.unsupported {
		return nil, p.diagnostics, ErrDTCGUnsupported
	}
	// Evidence arrays have no semantic ordering. Sort copies for repeatable bytes;
	// fallback families, shadow layers and cubic coordinates retain their order.
	assets, faces := slices.Clone(s.Assets), slices.Clone(s.Faces)
	slices.SortFunc(assets, func(a, b design.Asset) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(faces, func(a, b design.FontFace) int { return strings.Compare(a.ID, b.ID) })
	metadata := map[string]any{"profile": "dtcg-2025.10.v1", "mode": mode, "diagnostics": p.diagnostics}
	if len(assets) != 0 {
		metadata["assets"] = assets
	}
	if len(faces) != 0 {
		metadata["faces"] = faces
	}
	p.document["$extensions"] = map[string]any{dtcgExtension: metadata}
	data, err := json.MarshalIndent(p.document, "", "  ")
	return data, p.diagnostics, err
}

// Escape the escape marker first so 0.5 and 0%2E5 cannot alias. All segments use
// the same reversible encoding; sourcePath records the original identities.
func dtcgName(name string) string {
	return strings.NewReplacer("%", "%25", ".", "%2E", "{", "%7B", "}", "%7D", "$", "%24").Replace(name)
}

func dtcgColor(color design.SRGBA) map[string]any {
	return map[string]any{"colorSpace": "srgb", "components": [3]float64{color[0], color[1], color[2]}, "alpha": color[3]}
}
