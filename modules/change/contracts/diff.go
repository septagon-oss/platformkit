package contracts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"database/sql/driver"
)

// Diff is the change itself: an RFC 7386 JSON Merge Patch document. It is a map
// rather than a list of operations on purpose — merge patch says "this key now
// holds this value, and this other key is gone", which is what a reviewer reads,
// and its apply is a recursive merge over maps rather than an index arithmetic
// over arrays. A value of null removes the key; anything else replaces it.
//
// It decodes with UseNumber so `1` and `1.0` stay two numbers: a number that
// goes through float64 comes back as `1` for both, and a digest that cannot tell
// them apart is a digest two reviewers agreed on while approving different bytes.
type Diff map[string]any

// UnmarshalJSON reads a merge-patch document, keeping every number exactly as it
// was written. See Diff.
func (d *Diff) UnmarshalJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var raw map[string]json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return fmt.Errorf("change: a diff is an RFC 7386 merge-patch object: %w", err)
	}
	out := make(Diff, len(raw))
	for k, v := range raw {
		parsed, err := decodeNumber(v)
		if err != nil {
			return fmt.Errorf("change: the %s field of a diff: %w", k, err)
		}
		out[k] = parsed
	}
	*d = out
	return nil
}

// decodeNumber reads one value with numbers left as the text they arrived as.
func decodeNumber(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// Value and Scan store the diff as one jsonb column, the way modules/site spells
// its navigation: the codec is written once, on the named type. An absent diff is
// stored as an empty object, which is the patch that changes nothing.
func (d Diff) Value() (driver.Value, error) {
	if d == nil {
		return []byte("{}"), nil
	}
	canonical, err := d.canonical()
	if err != nil {
		return nil, err
	}
	return canonical, nil
}

func (d *Diff) Scan(src any) error {
	var raw []byte
	switch v := src.(type) {
	case nil:
		*d = Diff{}
		return nil
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return fmt.Errorf("change: a diff is jsonb and this is %T", src)
	}
	return d.UnmarshalJSON(raw)
}

// Canonical is the one byte sequence that stands for this diff: the document
// re-encoded with object keys in the order encoding/json sorts them, no
// indentation, no HTML escaping and no trailing newline. Two diffs that mean the
// same thing encode to the same bytes, which is what makes the digest below
// something a reviewer can be held to.
func (d Diff) Canonical() ([]byte, error) { return d.canonical() }

func (d Diff) canonical() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(map[string]any(d)); err != nil {
		return nil, fmt.Errorf("change: this diff cannot be written down: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Digest names the bytes: "sha256:" and the hex of the canonical form. A verdict
// is about a digest, and a row carries the digest it was made against, so "what
// did you approve" has an answer that does not depend on anybody's memory.
func (d Diff) Digest() (string, error) {
	canonical, err := d.canonical()
	if err != nil {
		return "", err
	}
	return DigestOf(canonical), nil
}

// DigestOf is the digest of bytes already in canonical form.
func DigestOf(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Merge applies this patch to a JSON document and returns the result. The rules
// are RFC 7386 in full and nothing else: an object value merges recursively, a
// null deletes the key, anything else replaces the value, and a patch that is not
// an object replaces the whole document.
func (d Diff) Merge(current []byte) ([]byte, error) {
	base := map[string]any{}
	if len(current) > 0 {
		dec := json.NewDecoder(bytes.NewReader(current))
		dec.UseNumber()
		if err := dec.Decode(&base); err != nil {
			return nil, fmt.Errorf("change: the document a merge patch applies to is a JSON object: %w", err)
		}
	}
	merged := mergeMaps(base, map[string]any(d))
	out, err := marshalValue(merged)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// mergeMaps is the whole of RFC 7386's apply, over the maps decoding gives back.
// Arrays and scalars are replaced rather than merged, because merge patch has no
// rule for an array and inventing one here would be a patch format of our own.
func mergeMaps(base, patch map[string]any) map[string]any {
	for key, value := range patch {
		if value == nil {
			delete(base, key)
			continue
		}
		if sub, ok := value.(map[string]any); ok {
			if existing, ok := base[key].(map[string]any); ok {
				base[key] = mergeMaps(existing, sub)
				continue
			}
			base[key] = mergeMaps(map[string]any{}, sub)
			continue
		}
		base[key] = value
	}
	return base
}

// marshalValue is the one encoder every byte this package writes goes through,
// so that the bytes in a digest, the bytes in a column and the bytes sent to a
// subject are produced by the same code or the whole idea fails.
func marshalValue(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("change: the merged document cannot be written down: %w", err)
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
