package main

// The native token projection is the checked-in copy of what a device consumes,
// the way apps/platformkit/testdata/asyncapi.json is the checked-in copy of the
// event contract and ui/screens/testdata/catalog.json is the checked-in copy of
// the catalog body. A native shell builds its palette from `go run ./tools/designexport
// | jq '{schema, themes}'`; until now the only thing that noticed when that copy
// went stale was a scheduled job in the repository holding the copy, which reports
// and never blocks. The producer is the one place both files can be seen, so the
// gate lives here: make check renders the export through the same run() the CLI
// serves and refuses a projection that is not the bytes committed beside it.
//
// UPDATE_GOLDEN=1 go test ./tools/designexport -run NativeTokenProjection rewrites
// the projection; the provenance record beside it has to be rewritten in the same
// commit, which the second test below refuses otherwise.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

const (
	// nativeTokenProjection is the file a native shell copies. Its key set is the
	// key set the shell's own scripts/tokens.ts names as its input.
	nativeTokenProjection = "testdata/design-tokens.json"
	// nativeTokenSource is the provenance record for those bytes, in the shape the
	// shell already parses for its catalog copy (platformkit.catalog-source.v1).
	nativeTokenSource = "testdata/design-tokens.source.json"
	// nativeTokenSchema is the export schema the projection repeats: the shell
	// refuses a projection that says anything else.
	nativeTokenSchema = "platformkit.design-export.v1"
)

// TestTheNativeTokenProjectionIsTheRenderedExport is the staleness gate: the
// checked-in projection equals what this tree's own designexport emits, projected
// to the two keys a device reads. It is in make check because make check runs
// every package of this module; no Makefile target and no new script is involved.
func TestTheNativeTokenProjectionIsTheRenderedExport(t *testing.T) {
	t.Parallel()
	got := renderNativeTokenProjection(t)

	if os.Getenv("UPDATE_GOLDEN") != "" {
		if err := os.WriteFile(nativeTokenProjection, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", nativeTokenProjection, err)
		}
		t.Logf("rewrote %s; rewrite %s with it", nativeTokenProjection, nativeTokenSource)
		return
	}

	want, err := os.ReadFile(nativeTokenProjection)
	if err != nil {
		t.Fatalf("read %s: %v (run with UPDATE_GOLDEN=1)", nativeTokenProjection, err)
	}
	if !bytes.Equal(want, got) {
		t.Fatalf("%s is stale; run with UPDATE_GOLDEN=1 and rewrite %s in the same commit.\nfirst difference at byte %d (%d bytes checked in, %d rendered)",
			nativeTokenProjection, nativeTokenSource, firstDifference(want, got), len(want), len(got))
	}
}

// TestTheNativeTokenProjectionCarriesItsProvenanceRecord refuses bytes and a
// record that disagree. The record is what makes the copy in the shell's
// repository checkable at all — a fixture with no hash is a fixture nobody can
// prove they have — so a stale hash is refused in both modes: UPDATE_GOLDEN=1
// rewrites the bytes and leaves the record pointing at bytes that no longer
// exist, which is the drift this gate exists to catch.
func TestTheNativeTokenProjectionCarriesItsProvenanceRecord(t *testing.T) {
	t.Parallel()
	body, err := os.ReadFile(nativeTokenSource)
	if err != nil {
		t.Fatalf("read %s: %v", nativeTokenSource, err)
	}
	var record struct {
		Schema   string            `json:"schema"`
		Fixture  string            `json:"fixture"`
		SHA256   string            `json:"sha256"`
		Upstream map[string]string `json:"upstream"`
	}
	if err := json.Unmarshal(body, &record); err != nil {
		t.Fatalf("%s is not JSON: %v", nativeTokenSource, err)
	}
	if record.Schema != "platformkit.design-export-source.v1" {
		t.Errorf("%s says schema %q", nativeTokenSource, record.Schema)
	}
	if record.Fixture != nativeTokenProjection {
		t.Errorf("%s names fixture %q, which is not the projection beside it (%s)", nativeTokenSource, record.Fixture, nativeTokenProjection)
	}
	fixture, err := os.ReadFile(nativeTokenProjection)
	if err != nil {
		t.Fatalf("read %s: %v", nativeTokenProjection, err)
	}
	sum := sha256.Sum256(fixture)
	if got := hex.EncodeToString(sum[:]); record.SHA256 != got {
		t.Errorf("%s is stale: %s hashes to %s, the record says %s", nativeTokenSource, nativeTokenProjection, got, record.SHA256)
	}

	// The producer pins the repository and the path, never a revision: the kernel
	// has no tag at this commit, and a record naming one would be wrong at the
	// next commit and would train everyone to ignore this gate. The shell's own
	// refresh step adds upstream.commit and upstream.tag at the moment it copies
	// the file, which is the moment those two keys mean something.
	for key, value := range record.Upstream {
		if value == "" {
			t.Errorf("%s's upstream.%s is empty", nativeTokenSource, key)
		}
	}
	for _, banned := range []string{"commit", "tag"} {
		if _, ok := record.Upstream[banned]; ok {
			t.Errorf("%s pins upstream.%s, which the producer cannot know before it tags; the refresh step that copies the bytes adds it", nativeTokenSource, banned)
		}
	}
	for _, want := range []string{"repository", "path", "command"} {
		if record.Upstream[want] == "" {
			t.Errorf("%s has no upstream.%s", nativeTokenSource, want)
		}
	}
	// The command is the line the shell already runs (its scripts/tokens.ts names
	// it); the record that names a different one sends a person to a recipe that
	// does not produce these bytes.
	if record.Upstream["command"] != "go run ./tools/designexport | jq '{schema, themes}'" {
		t.Errorf("upstream.command = %q", record.Upstream["command"])
	}
	if record.Upstream["repository"] != "https://github.com/septagon-oss/platformkit" {
		t.Errorf("upstream.repository = %q", record.Upstream["repository"])
	}
	if record.Upstream["path"] != "tools/designexport/"+nativeTokenProjection {
		t.Errorf("upstream.path = %q, which is not where these bytes live in this repository", record.Upstream["path"])
	}
}

// TestTheNativeTokenProjectionIsTheTwoKeysAndBothModes states what the projection
// owes a device: the two keys and nothing else, both modes, the same token names
// in each, and the three families the shell's generator reads its font stack from.
// A projection that lost a token name in one mode would generate a partial palette
// on the phone — the failure the shell's own scripts/tokens.ts refuses at the far
// end, refused here at the near one where the cause is.
func TestTheNativeTokenProjectionIsTheTwoKeysAndBothModes(t *testing.T) {
	t.Parallel()
	rendered := renderNativeTokenProjection(t)

	// The key set, read off the bytes rather than off a struct, so a key the shell
	// never asked for cannot ride along unnoticed.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(rendered, &keys); err != nil {
		t.Fatalf("the projection is not JSON: %v", err)
	}
	var names []string
	for key := range keys {
		names = append(names, key)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"schema", "themes"}) {
		t.Fatalf("the projection carries %s, not the two keys a device reads (schema, themes)", strings.Join(names, ", "))
	}
	if string(keys["schema"]) != `"`+nativeTokenSchema+`"` {
		t.Errorf("projection schema = %s, want %q", keys["schema"], nativeTokenSchema)
	}

	var doc struct {
		Themes []struct {
			Mode   string `json:"mode"`
			Name   string `json:"name"`
			Tokens []struct {
				Name  string `json:"name"`
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"tokens"`
		} `json:"themes"`
	}
	if err := json.Unmarshal(rendered, &doc); err != nil {
		t.Fatalf("the projection's themes are not what a device parses: %v", err)
	}
	if len(doc.Themes) != 2 {
		t.Fatalf("%d themes; a device with one theme has one palette and no dark mode", len(doc.Themes))
	}
	var first []string
	for i, theme := range doc.Themes {
		if theme.Mode != []string{"light", "dark"}[i] {
			t.Errorf("theme %d is mode %q", i, theme.Mode)
		}
		if theme.Name == "" {
			t.Errorf("theme %d has no name", i)
		}
		names := make([]string, 0, len(theme.Tokens))
		for _, token := range theme.Tokens {
			if token.Name == "" || token.Type == "" || token.Value == "" {
				t.Errorf("%s: a token with an empty name, type or value: %+v", theme.Mode, token)
			}
			names = append(names, token.Name)
		}
		if first == nil {
			first = names
			continue
		}
		if !slices.Equal(first, names) {
			t.Errorf("%s does not carry the token names light carries: a shell that generates one palette from both modes gets a partial one", theme.Mode)
		}
	}
	// The shell's generator names these three (its FONTS constant); a projection
	// without one is a phone drawing its display face from a fallback list.
	for _, font := range []string{"--pk-font-display", "--pk-font-body", "--pk-font-mono"} {
		if !slices.Contains(first, font) {
			t.Errorf("no %s in the projection", font)
		}
	}
}

// renderNativeTokenProjection runs the CLI's own entry point — the one path that
// produces the export a device consumes — and keeps the two keys a shell reads.
// It renders from the tree, never from the file beside it, so a broken producer
// cannot pass by rewriting the golden to match itself.
func renderNativeTokenProjection(t *testing.T) []byte {
	t.Helper()
	var full bytes.Buffer
	if err := run(nil, new(failingReader), &full); err != nil {
		t.Fatalf("designexport: %v", err)
	}
	return nativeTokenProjectionOf(t, full.Bytes())
}

// nativeTokenProjectionOf is the `{schema, themes}` of the pipeline, in Go: the
// two keys, in that order, with every theme's bytes kept as the export emitted
// them. The shell reads the file, not this function, so the shape is checked
// against the key set its scripts/tokens.ts names.
func nativeTokenProjectionOf(t *testing.T, exported []byte) []byte {
	t.Helper()
	var doc struct {
		Schema string            `json:"schema"`
		Themes []json.RawMessage `json:"themes"`
	}
	if err := json.Unmarshal(exported, &doc); err != nil {
		t.Fatalf("designexport emitted a document this projection cannot read: %v", err)
	}
	if doc.Schema != nativeTokenSchema {
		t.Fatalf("designexport emitted schema %q, not %q", doc.Schema, nativeTokenSchema)
	}
	out, err := json.Marshal(struct {
		Schema string            `json:"schema"`
		Themes []json.RawMessage `json:"themes"`
	}{doc.Schema, doc.Themes})
	if err != nil {
		t.Fatalf("project the export: %v", err)
	}
	var indented bytes.Buffer
	if err := json.Indent(&indented, out, "", "  "); err != nil {
		t.Fatalf("indent the projection: %v", err)
	}
	indented.WriteByte('\n')
	return indented.Bytes()
}

// firstDifference is where two byte-for-byte artefacts first part company, which
// is the one fact worth printing when a golden is stale: the offset is usually
// enough to find the changed token or operation without opening a diff tool.
func firstDifference(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// TestTheProjectionOfAnExportIsTheTwoKeys checks the projection itself against
// bytes that carry more than the two keys, so the gate cannot be passed by a
// producer that stopped emitting the rest of the export.
func TestTheProjectionOfAnExportIsTheTwoKeys(t *testing.T) {
	t.Parallel()
	in := []byte(`{"schema":"platformkit.design-export.v1","css":"body{}","themes":[{"mode":"light","name":"light","tokens":[{"name":"--pk-x","type":"color","value":"#fff"}]}],"examples":[]}`)
	got := nativeTokenProjectionOf(t, in)
	want := "{\n  \"schema\": \"platformkit.design-export.v1\",\n  \"themes\": [\n    {\n      \"mode\": \"light\",\n      \"name\": \"light\",\n      \"tokens\": [\n        {\n          \"name\": \"--pk-x\",\n          \"type\": \"color\",\n          \"value\": \"#fff\"\n        }\n      ]\n    }\n  ]\n}\n"
	if string(got) != want {
		t.Errorf("projection =\n%s\nwant\n%s", got, want)
	}
}
