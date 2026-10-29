package screens_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// parityFixture is one reviewed case; see testdata/parity/README.md.
type parityFixture struct {
	Name       string `json:"name"`
	Provenance struct {
		Author       string `json:"author"`
		Date         string `json:"date"`
		InputHash    string `json:"inputHash"`
		ExpectedHash string `json:"expectedHash"`
	} `json:"provenance"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
}

type parityInput struct {
	Language     string                 `json:"language"`
	Schema       entity.Schema          `json:"schema"`
	Hints        entity.EntryHints      `json:"hints"`
	Immutable    []string               `json:"immutable"`
	Singleton    bool                   `json:"singleton"`
	CommandsOnly bool                   `json:"commandsOnly"`
	Operations   []string               `json:"operations"`
	Commands     []present.CommandInput `json:"commands"`
	Translations map[string]string      `json:"translations"`
	Rows         []map[string]any       `json:"rows"`
}

type parityRow struct {
	ListTitle   string            `json:"listTitle"`
	RecordTitle string            `json:"recordTitle"`
	Cells       map[string]string `json:"cells"`
}

// canonicalHash is sha256 of the compact JSON of a member decoded to `any`,
// re-encoded with sorted keys and HTML escaping off, so a client in another
// language computes the same bytes.
func canonicalHash(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimRight(b.Bytes(), "\n"))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func verifyProvenance(f parityFixture, file string) error {
	if f.Name != strings.TrimSuffix(filepath.Base(file), ".json") {
		return fmt.Errorf("name %q is not the file name", f.Name)
	}
	if f.Provenance.Author == "" || f.Provenance.Date == "" {
		return fmt.Errorf("provenance names no author or date")
	}
	for _, c := range []struct {
		raw  json.RawMessage
		want string
		what string
	}{{f.Input, f.Provenance.InputHash, "input"}, {f.Expected, f.Provenance.ExpectedHash, "expected"}} {
		got, err := canonicalHash(c.raw)
		if err != nil {
			return err
		}
		if got != c.want {
			return fmt.Errorf("%s changed without its provenance: hash is %s", c.what, got)
		}
	}
	return nil
}

// subset reports the first place want holds something got does not: objects
// are compared by want's keys, arrays by length and element.
func subset(path string, want, got any) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return path + ": not an object"
		}
		for k, wv := range w {
			gv, present := g[k]
			if !present {
				return path + "." + k + ": missing"
			}
			if d := subset(path+"."+k, wv, gv); d != "" {
				return d
			}
		}
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return fmt.Sprintf("%s: want %v got %v", path, want, got)
		}
		for i := range w {
			if d := subset(fmt.Sprintf("%s[%d]", path, i), w[i], g[i]); d != "" {
				return d
			}
		}
	default:
		if !reflect.DeepEqual(want, got) {
			return fmt.Sprintf("%s: want %v got %v", path, want, got)
		}
	}
	return ""
}

func (in parityInput) resolve() present.Resolved {
	return present.Resolve(present.Input{
		Schema: in.Schema, Hints: in.Hints, Immutable: in.Immutable, Singleton: in.Singleton,
		CommandsOnly: in.CommandsOnly, Operations: in.Operations, Commands: in.Commands,
		Language: in.Language,
		Text: func(key, fallback string) string {
			if words, ok := in.Translations[key]; ok {
				return words
			}
			return fallback
		},
	})
}

func readParity(t *testing.T) map[string]parityFixture {
	t.Helper()
	files, err := filepath.Glob("testdata/parity/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no parity fixtures: %v", err)
	}
	out := map[string]parityFixture{}
	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var f parityFixture
		if err := json.Unmarshal(raw, &f); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if err := verifyProvenance(f, file); err != nil {
			t.Errorf("%s: %v", file, err)
		}
		out[file] = f
	}
	return out
}

func TestParityFixturesMatchTheProductionResolver(t *testing.T) {
	t.Parallel()
	fixtures := readParity(t)
	if len(fixtures) < 12 {
		t.Fatalf("%d parity fixtures, want at least 12", len(fixtures))
	}
	for file, f := range fixtures {
		t.Run(f.Name, func(t *testing.T) {
			var in parityInput
			if err := json.Unmarshal(f.Input, &in); err != nil {
				t.Fatal(err)
			}
			var want struct {
				Resolved json.RawMessage      `json:"resolved"`
				Rows     map[string]parityRow `json:"rows"`
			}
			if err := json.Unmarshal(f.Expected, &want); err != nil {
				t.Fatal(err)
			}
			resolved := in.resolve()
			var got, expected any
			encoded, _ := json.Marshal(resolved)
			_ = json.Unmarshal(encoded, &got)
			_ = json.Unmarshal(want.Resolved, &expected)
			if d := subset("resolved", expected, got); d != "" {
				t.Errorf("%s: %s", file, d)
			}
			checkRows(t, in, resolved, want.Rows)
		})
	}
}

func checkRows(t *testing.T, in parityInput, resolved present.Resolved, want map[string]parityRow) {
	t.Helper()
	for _, row := range in.Rows {
		id, _ := row["id"].(string)
		w, ok := want[id]
		if !ok {
			t.Errorf("row %q has no expectation", id)
			continue
		}
		empty := resolved.Titles.EmptyTitle
		if got := present.Title(resolved.Titles.ListCandidates, row, id, empty); got != w.ListTitle {
			t.Errorf("row %q list title %q, want %q", id, got, w.ListTitle)
		}
		if got := present.Title(resolved.Titles.RecordCandidates, row, id, empty); got != w.RecordTitle {
			t.Errorf("row %q record title %q, want %q", id, got, w.RecordTitle)
		}
		for name, cell := range w.Cells {
			f, _ := entity.FieldNamed(in.Schema.Fields, name)
			if got := display.Display(f, row[name]); got != cell {
				t.Errorf("row %q cell %s %q, want %q", id, name, got, cell)
			}
		}
	}
}

func TestParityFixtureProvenanceIsEnforced(t *testing.T) {
	t.Parallel()
	for file, f := range readParity(t) {
		f.Input = json.RawMessage(string(f.Input) + " ")
		if err := verifyProvenance(f, file); err != nil {
			t.Fatalf("whitespace is not a change: %v", err)
		}
		var edited map[string]any
		if err := json.Unmarshal(f.Input, &edited); err != nil {
			t.Fatal(err)
		}
		edited["language"] = "xx"
		f.Input, _ = json.Marshal(edited)
		if err := verifyProvenance(f, file); err == nil {
			t.Fatalf("%s: an edited input passed its provenance", file)
		}
		return
	}
}
