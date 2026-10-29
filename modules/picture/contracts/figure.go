package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"slices"
)

// Figure is immutable validated SVG bound to finite choice IDs. Zero is unusable.
// Its digest identifies canonical bytes; Render never changes the declaration.
type Figure struct {
	data   *declaration
	ids    []string
	svg    string
	digest [32]byte
}

// CompileFigure uses the shared SVG grammar with data-choice and bounded leaf text.
// Every supplied ID must be mapped, with no nested markers or unknown IDs.
func CompileFigure(name, source string, ids []string) (Figure, error) {
	fail := func(why string) (Figure, error) { return Figure{}, refusal(name, "figure", why) }
	if len(ids) == 0 || len(ids) > 256 {
		return fail("supply 1–256 ordinary choice IDs")
	}
	allowed := map[string]bool{}
	for _, id := range ids {
		if !validName(id) || allowed[id] {
			return fail("choice IDs must be valid and unique")
		}
		allowed[id] = true
	}
	decl, err := compile(name, source, true)
	if err != nil {
		return Figure{}, err
	}
	mapped := map[string]bool{}
	var visit func(*element)
	visit = func(e *element) {
		if id := e.attrs["data-choice"]; id != "" {
			mapped[id] = true
		}
		for _, child := range e.children {
			visit(child)
		}
	}
	visit(decl.root)
	if !maps.Equal(allowed, mapped) {
		return fail("map every ordinary choice and no other ID")
	}
	svg, err := encode(decl.root, nil, func(*element, map[string]string) bool { return true })
	if err != nil {
		return Figure{}, err
	}
	return Figure{data: decl, ids: slices.Sorted(maps.Keys(mapped)), svg: svg, digest: sha256.Sum256([]byte(svg))}, nil
}

// ChoiceIDs returns detached, byte-sorted mapped IDs; zero returns an empty list.
func (f Figure) ChoiceIDs() []string { return append([]string{}, f.ids...) }
func (f Figure) Digest() [32]byte    { return f.digest }
func (f Figure) SVG() string         { return f.svg }

// Render highlights a unique subset. Unknown IDs and zero values return no bytes.
func (f Figure) Render(selected []string) (string, error) {
	if f.data == nil {
		return "", errors.New("picture: a compiled figure is required")
	}
	ids := map[string]bool{}
	for _, id := range selected {
		if !slices.Contains(f.ids, id) || ids[id] {
			return "", errors.New("picture: select each mapped choice at most once")
		}
		ids[id] = true
	}
	return encode(f.data.root, nil, func(e *element, attrs map[string]string) bool {
		if ids[attrs["data-choice"]] {
			attrs["data-selected"] = "true"
		}
		return true
	})
}

type figureJSON struct {
	Digest    string   `json:"digest"`
	SVG       string   `json:"svg"`
	ChoiceIDs []string `json:"choiceIDs"`
}

func (f Figure) MarshalJSON() ([]byte, error) {
	if f.data == nil {
		return nil, errors.New("picture: cannot encode a zero figure")
	}
	return json.Marshal(figureJSON{hex.EncodeToString(f.digest[:]), f.svg, f.ChoiceIDs()})
}

// UnmarshalJSON recompiles and verifies every canonical member, rejecting unknown
// or duplicate keys. Failure leaves the receiver unchanged.
func (f *Figure) UnmarshalJSON(raw []byte) error {
	bad := errors.New("picture: figure JSON must contain verified canonical SVG, digest and choiceIDs")
	if len(raw) > 2*1048576 {
		return bad
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return bad
	}
	members := map[string]json.RawMessage{}
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return bad
		}
		key, ok := token.(string)
		if !ok || members[key] != nil || !slices.Contains([]string{"digest", "svg", "choiceIDs"}, key) {
			return bad
		}
		var value json.RawMessage
		if err := d.Decode(&value); err != nil {
			return bad
		}
		members[key] = value
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') {
		return bad
	}
	if _, err := d.Token(); err != io.EOF || len(members) != 3 {
		return bad
	}
	var v figureJSON
	if err := json.Unmarshal(raw, &v); err != nil {
		return bad
	}
	compiled, err := CompileFigure("figure", v.SVG, v.ChoiceIDs)
	if err != nil || v.SVG != compiled.svg || v.Digest != hex.EncodeToString(compiled.digest[:]) || !slices.Equal(v.ChoiceIDs, compiled.ids) {
		return bad
	}
	*f = compiled
	return nil
}
