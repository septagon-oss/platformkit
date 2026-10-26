package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"slices"
	"strings"
)

// Selection supplies exactly one declared option for a part.
type Selection struct {
	Part   string `json:"part"`
	Option string `json:"option"`
}

// Value supplies all selections, slot colours in declaration order, and a background.
type Value struct {
	Options    []Selection `json:"options"`
	Colours    []string    `json:"colours"`
	Background string      `json:"background"`
}

// Resolved holds private canonical inputs bound to one declaration digest.
type Resolved struct {
	digest    [32]byte
	identity  [32]byte
	canonical string
	value     Value
}

// Canonical returns detached fixed-order JSON; a zero value returns nil.
func (r Resolved) Canonical() []byte {
	if r.canonical == "" {
		return nil
	}
	return []byte(r.canonical)
}

// Identity returns the domain-separated render digest, or zero for an unresolved value.
func (r Resolved) Identity() [32]byte { return r.identity }

// Resolve validates and copies the complete bounded value. Selection order and
// hexadecimal case normalize; slot order remains significant. Refusals return zero.
func Resolve(t Template, v Value) (Resolved, error) {
	fail := func(s string) (Resolved, error) { return Resolved{}, errors.New("picture: " + s + ".") }
	if t.data == nil {
		return fail("a compiled template is required")
	}
	if len(v.Options) != len(t.data.parts) || len(v.Colours) != len(t.data.slots) {
		return fail("supply exactly one option per part and colour per slot")
	}
	for _, s := range v.Options {
		if len(s.Part) > 32 || len(s.Option) > 32 {
			return fail("selection names exceed 32 bytes")
		}
	}
	if !colourPattern.MatchString(v.Background) {
		return fail("background must be #RRGGBB")
	}
	for _, c := range v.Colours {
		if !colourPattern.MatchString(c) {
			return fail("slot colours must be #RRGGBB")
		}
	}
	copyValue := Value{Options: append([]Selection{}, v.Options...), Colours: append([]string{}, v.Colours...), Background: strings.ToUpper(v.Background)}
	slices.SortFunc(copyValue.Options, func(a, b Selection) int { return strings.Compare(a.Part, b.Part) })
	for i, s := range copyValue.Options {
		p := t.data.parts[i]
		if s.Part != p.Name || !slices.Contains(p.Options, s.Option) {
			return fail("select each declared part once using its own options")
		}
	}
	for i := range copyValue.Colours {
		copyValue.Colours[i] = strings.ToUpper(copyValue.Colours[i])
	}
	canonical, err := json.Marshal(copyValue)
	if err != nil || len(canonical) > 8192 {
		return fail("canonical value exceeds 8192 bytes")
	}
	h := sha256.New()
	h.Write([]byte("platformkit.picture.v1\x00"))
	h.Write(t.data.digest[:])
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(canonical)))
	h.Write(length[:])
	h.Write(canonical)
	var identity [32]byte
	copy(identity[:], h.Sum(nil))
	return Resolved{digest: t.data.digest, identity: identity, canonical: string(canonical), value: copyValue}, nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1048576 {
		return 0, errors.New("picture: output exceeds 1048576 bytes")
	}
	return b.Buffer.Write(p)
}

// Compose serializes immutable resolved inputs using XML tokens. Zero or
// mismatched inputs and output over 1 MiB fail without returning partial output.
func Compose(t Template, r Resolved) (string, error) {
	if t.data == nil || r.canonical == "" || r.digest != t.data.digest {
		return "", errors.New("picture: resolved value does not belong to this template")
	}
	selections := map[string]string{}
	for _, s := range r.value.Options {
		selections[s.Part] = s.Option
	}
	colours := map[string]string{}
	for i, s := range t.data.slots {
		colours[s] = r.value.Colours[i]
	}

	b := t.data.box
	bg := &element{name: "rect", attrs: map[string]string{"x": decimal(b[0]), "y": decimal(b[1]), "width": decimal(b[2]), "height": decimal(b[3]), "fill": r.value.Background}}
	return encode(t.data.root, bg, func(e *element, attrs map[string]string) bool {
		if p := attrs["data-part"]; p != "" && selections[p] != attrs["data-option"] {
			return false
		}
		if s := attrs["data-slot"]; s != "" {
			if slot, ok := strings.CutPrefix(s, "stroke:"); ok {
				attrs["stroke"] = colours[slot]
			} else {
				attrs["fill"] = colours[s]
			}
		}
		delete(attrs, "data-part")
		delete(attrs, "data-option")
		delete(attrs, "data-slot")
		return true
	})
}
