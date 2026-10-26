package screens_test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// pageCase is one registration, the presentation states a shell may ask it about, and
// the Page each state answers; see testdata/page/README.md. Its input is the reviewed
// parity case's input plus the "state" list, so the reader that resolves the reviewed
// policy resolves this one.
type pageCase struct {
	parityInput
	Name     string           `json:"-"`
	File     string           `json:"-"`
	States   []pageState      `json:"state"`
	Expected pageExpectations `json:"-"`
}

// pageState is the fixed state of one build. The README's defaults are writable,
// counted, one page of fifty rows, and the rows it carries as its own total.
type pageState struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind"`
	Now        string            `json:"now"`
	Home       string            `json:"home"`
	FormTitle  string            `json:"formTitle"`
	Sort       string            `json:"sort"`
	Detail     string            `json:"detail"`
	Whole      bool              `json:"whole"`
	Create     bool              `json:"create"`
	Writable   *bool             `json:"writable"`
	Counted    *bool             `json:"counted"`
	Window     *view.Window      `json:"window"`
	Total      *int64            `json:"total"`
	Violations map[string]string `json:"violations"`
}

// written is the state's member or the README's default for it.
func written[T any](got *T, absent T) T { return *cmp.Or(got, &absent) }

// build adapts the case to the pure builder: the resource's half of the description
// comes from the registration, the policy half from present.Resolve. The fixture names no
// command scope, so every command is described as the collection's, the reading that
// draws it on a list and hides it on a record.
func (c pageCase) build(s pageState) (view.Page, error) {
	described := make([]view.CommandDescription, 0, len(c.Commands))
	for _, cmd := range c.Commands {
		described = append(described, view.CommandDescription{
			Verb: cmd.Verb, Collection: true, Undo: cmd.Undo, HasResult: cmd.HasResult})
	}
	now, err := time.Parse(time.RFC3339, s.Now)
	if err != nil {
		return view.Page{}, err
	}
	return view.Build(view.Input{
		Resource: view.Description{Resource: c.Schema.Module + "/" + c.Schema.Entity,
			Schema: c.Schema, Operations: c.Operations, Singleton: c.Singleton,
			Immutable: c.Immutable, Commands: described},
		Policy: c.resolve(),
		Data: view.Data{Records: c.Rows, Window: written(s.Window, view.Window{Limit: 50}),
			Total: written(s.Total, int64(len(c.Rows))), Counted: written(s.Counted, true),
			Sort: s.Sort, Violations: s.Violations, Detail: s.Detail},
		State: view.State{Kind: view.Kind(s.Kind), Language: c.Language, Now: now,
			Writable: written(s.Writable, true), Home: s.Home,
			Form: view.FormState{Create: s.Create, Title: s.FormTitle}}})
}

// readPageCases reads the family, refusing a case that does not carry its own provenance,
// and writes nothing.
func readPageCases(t *testing.T) []pageCase {
	t.Helper()
	files, err := filepath.Glob("testdata/page/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no page fixtures: %v", err)
	}
	var cases []pageCase
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var holder parityFixture
		if err := json.Unmarshal(raw, &holder); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if err := verifyProvenance(holder, file); err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		c := pageCase{Name: holder.Name, File: file}
		// The rows reach the builder as the platform's own reader decodes them, with
		// numbers kept whole, so a case cannot pass on a spelling no caller sends.
		reader := json.NewDecoder(bytes.NewReader(holder.Input))
		reader.UseNumber()
		if err := reader.Decode(&c); err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		if err := json.Unmarshal(holder.Expected, &c.Expected); err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		cases = append(cases, c)
	}
	return cases
}

// TestPageFixturesMatchThePureBuilder is the brief's "Build matches every fixture": each
// state of each case is built by the production builder and compared with its expectation
// — a subset of what the case is about, or byte for byte where the state says "whole". A
// state expected to be refused is built too, because the refusal is what has to happen.
func TestPageFixturesMatchThePureBuilder(t *testing.T) {
	t.Parallel()
	cases := readPageCases(t)
	if len(cases) < 21 {
		t.Fatalf("%d page fixtures, want the 17 reviewed cases and the whole goldens", len(cases))
	}
	for _, c := range cases {
		for _, s := range c.States {
			t.Run(filepath.Base(c.File)+" "+s.Name, func(t *testing.T) { c.check(t, s) })
		}
	}
}

// check builds one state against its expectation. A whole state compares with the exact
// marshalled bytes, so member order, key presence and every value spelling are pinned.
func (c pageCase) check(t *testing.T, s pageState) {
	t.Helper()
	want, ok := c.Expected.Page[s.Name]
	if !ok {
		t.Fatalf("%s: state %q has no expectation", c.File, s.Name)
	}
	var refusal struct{ Refused string }
	_ = json.Unmarshal(want, &refusal)
	page, err := c.build(s)
	if refusal.Refused != "" {
		var r view.Refusal
		if !errors.As(err, &r) {
			t.Fatalf("%s %s: want the refusal of %q, got a page (%v)", c.File, s.Name, refusal.Refused, err)
		}
		if r.Field != refusal.Refused {
			t.Errorf("%s %s: refused %q, want %q", c.File, s.Name, r.Field, refusal.Refused)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s %s: %v", c.File, s.Name, err)
	}
	got, _ := json.Marshal(page)
	var wantAny, gotAny any
	_ = json.Unmarshal(want, &wantAny)
	_ = json.Unmarshal(got, &gotAny)
	if s.Whole {
		var compact bytes.Buffer
		if err := json.Compact(&compact, want); err != nil {
			t.Fatalf("%s %s: %v", c.File, s.Name, err)
		}
		wantAny, gotAny = anyMap(t, compact.Bytes()), anyMap(t, got)
		if !bytes.Equal(compact.Bytes(), got) {
			t.Errorf("%s %s: not the reviewed bytes\nwant %s\n got %s", c.File, s.Name, compact.String(), got)
		}
	}
	if d := subset("page", wantAny, gotAny); d != "" {
		t.Errorf("%s %s: %s", c.File, s.Name, d)
	}
}

func anyMap(t *testing.T, raw []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

// TestAWholePageFixtureIsByteForByte asks that a whole golden stands for each page kind
// and holds exactly: a subset case says a page carries something, these say it carries
// nothing besides.
func TestAWholePageFixtureIsByteForByte(t *testing.T) {
	t.Parallel()
	kinds := map[string]int{}
	for _, c := range readPageCases(t) {
		for _, s := range c.States {
			if !s.Whole || len(c.Expected.Page[s.Name]) == 0 {
				continue
			}
			kinds[s.Kind]++
			c.check(t, s)
		}
	}
	for _, kind := range []string{"list", "record", "form"} {
		if kinds[kind] == 0 {
			t.Errorf("no whole %s golden, so nothing pins the exact bytes of one", kind)
		}
	}
}

// TestEveryPageFixtureIsAReviewedParityCase follows the family's rule: a case named for a
// reviewed parity case is built on that case's input, proved by hashing the input with its
// own "state" list removed rather than by a copied digest. A case named for nothing but
// itself — a whole golden, a value-domain row — carries its own input and is its own.
func TestEveryPageFixtureIsAReviewedParityCase(t *testing.T) {
	t.Parallel()
	for _, c := range readPageCases(t) {
		name := strings.TrimSuffix(filepath.Base(c.File), ".json")
		if c.Name != name {
			t.Errorf("%s names its case %q: a page case is named for the file it is in", c.File, c.Name)
		}
		raw, err := os.ReadFile(filepath.Join("testdata", "parity", name+".json"))
		if err != nil {
			continue
		}
		var reviewedCase parityFixture
		if err := json.Unmarshal(raw, &reviewedCase); err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		trimmed, _ := json.Marshal(withoutState(mustRead(t, c.File)))
		hash, err := canonicalHash(trimmed)
		if err != nil {
			t.Errorf("%s: %v", c.File, err)
		} else if hash != reviewedCase.Provenance.InputHash {
			t.Errorf("%s is not built on the reviewed input: %s over %s", c.File, hash, reviewedCase.Provenance.InputHash)
		}
	}
}

// TestPageFixtureProvenanceIsEnforced proves the hash over a case is a check and not a
// formality: whitespace is no change, one edited member is.
func TestPageFixtureProvenanceIsEnforced(t *testing.T) {
	t.Parallel()
	for _, c := range readPageCases(t) {
		var holder parityFixture
		if err := json.Unmarshal(mustRead(t, c.File), &holder); err != nil {
			t.Fatal(err)
		}
		holder.Input = json.RawMessage(string(holder.Input) + " ")
		if err := verifyProvenance(holder, c.File); err != nil {
			t.Errorf("%s: whitespace is not a change: %v", c.File, err)
		}
		var edited map[string]any
		_ = json.Unmarshal(holder.Input, &edited)
		edited["language"] = "xx"
		holder.Input, _ = json.Marshal(edited)
		if err := verifyProvenance(holder, c.File); err == nil {
			t.Errorf("%s: an edited input passed its provenance", c.File)
		}
		return
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
