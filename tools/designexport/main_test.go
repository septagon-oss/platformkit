package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

type failingReader struct{ called bool }

func (r *failingReader) Read([]byte) (int, error) {
	r.called = true
	return 0, io.ErrUnexpectedEOF
}

func exportedSnapshot(t *testing.T, args []string, input io.Reader) (ui.DesignExport, []byte) {
	t.Helper()
	var output bytes.Buffer
	if err := run(args, input, &output); err != nil {
		t.Fatal(err)
	}
	var snapshot ui.DesignExport
	if err := json.Unmarshal(output.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot, output.Bytes()
}

func TestExportReportsWriterFailure(t *testing.T) {
	t.Parallel()
	if err := run(nil, new(failingReader), failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write failure was lost: %v", err)
	}
}

func TestExportPreservesFullSnapshotWithoutReadingInput(t *testing.T) {
	t.Parallel()
	input := new(failingReader)
	_, first := exportedSnapshot(t, nil, input)
	_, again := exportedSnapshot(t, nil, input)
	want, err := ui.Export(design.Default(), components.Gallery())
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if input.called || !bytes.Equal(first, again) || !bytes.Equal(first, append(canonical, '\n')) {
		t.Fatal("full export read stdin, changed its existing bytes, or was not repeatable")
	}
}

func TestExportProjectsExistingTypedExamples(t *testing.T) {
	t.Parallel()
	input := new(failingReader)
	full, original := exportedSnapshot(t, nil, input)
	for _, tc := range []struct{ id, patch, html string }{
		{"pk-ui.component.button/primary", `{"label":"<Keep & copy>"}`, `&lt;Keep &amp; copy&gt;`},
		{"pk-ui.component.button/primary", `{"loading":true}`, `aria-busy="true"`},
		{"pk-ui.component.input/email", `{"value":"a&b@example.test"}`, `value="a&amp;b@example.test"`},
		{"pk-ui.component.skiplink/default", "", "Skip to content"},
	} {
		t.Run(tc.id+tc.patch, func(t *testing.T) {
			args := []string{"--example", tc.id}
			selected, _ := exportedSnapshot(t, args, input)
			index := slices.IndexFunc(full.Examples, func(e components.ExampleDescription) bool { return e.ID == tc.id })
			if index < 0 || len(selected.Examples) != 1 || !reflect.DeepEqual(selected.Examples[0], full.Examples[index]) {
				t.Fatal("selection changed the existing example or included other examples")
			}
			changed := selected
			if tc.patch != "" {
				changed, _ = exportedSnapshot(t, append(args, "--props"), strings.NewReader(tc.patch))
				if len(changed.Examples) != 1 || changed.SHA256 == selected.SHA256 {
					t.Fatal("property edit did not produce one content-addressed projection")
				}
				var props, patch map[string]any
				if json.Unmarshal(changed.Examples[0].Props, &props) != nil || json.Unmarshal([]byte(tc.patch), &patch) != nil {
					t.Fatal("projection did not retain valid property JSON")
				}
				for name, value := range patch {
					if !reflect.DeepEqual(props[name], value) {
						t.Fatalf("projected property %s = %v, want %v", name, props[name], value)
					}
				}
			}
			before, after := selected.Examples[0], changed.Examples[0]
			if !strings.Contains(after.HTML, tc.html) || before.ExampleInfo != after.ExampleInfo || !bytes.Equal(before.Schema, after.Schema) || !reflect.DeepEqual(before.Slots, after.Slots) {
				t.Fatal("projection lost expected rendering, identity, schema, or slots")
			}
			metadata := full
			metadata.Examples, selected.Examples, changed.Examples = nil, nil, nil
			metadata.SHA256, selected.SHA256, changed.SHA256 = "", "", ""
			if !reflect.DeepEqual(metadata, selected) || !reflect.DeepEqual(metadata, changed) {
				t.Fatal("selection or property editing changed canonical tokens, icons, CSS, or metadata")
			}
		})
	}
	_, repeated := exportedSnapshot(t, nil, input)
	if input.called || !bytes.Equal(original, repeated) {
		t.Fatal("projection read unrequested stdin or mutated source examples")
	}
}

func TestExportRejectsArgumentsBeforeReadingInput(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--output=snapshot.json"}, {"--props"}, {"--example"}, {"--example", ""},
		{"--example", "missing"}, {"--example", "missing", "--props"},
		{"--example", "pk-ui.component.button/primary", "--unknown"},
		{"--example", "pk-ui.component.button/primary", "--props", "extra"},
	} {
		var output bytes.Buffer
		input := new(failingReader)
		if err := run(args, input, &output); err == nil || output.Len() != 0 || input.called {
			t.Fatalf("rejected arguments %q read stdin or wrote output: %v", args, err)
		}
	}
}

func TestExportRejectsInvalidPropsWithoutOutput(t *testing.T) {
	t.Parallel()
	args := []string{"--example", "pk-ui.component.button/primary", "--props"}
	for _, body := range []string{
		"", "null", "[]", "{} {}", "{} trailing", `{"label":12}`, `{"loading":"true"}`,
		`{"Label":"wrong case"}`, `{"hx-post":"/mutate"}`, `{"class":"injected"}`,
		`{"label":"first","label":"second"}`, "{}" + strings.Repeat(" ", (1<<20)-1),
	} {
		var output bytes.Buffer
		if err := run(args, strings.NewReader(body), &output); err == nil || output.Len() != 0 {
			t.Fatalf("invalid property input (%d bytes) produced output: %v", len(body), err)
		}
	}
	if err := run(args, strings.NewReader("{}"+strings.Repeat(" ", (1<<20)-2)), io.Discard); err != nil {
		t.Fatalf("valid object at the one-MiB boundary was rejected: %v", err)
	}
	var output bytes.Buffer
	if err := run(args, io.MultiReader(strings.NewReader("{}"), new(failingReader)), &output); !errors.Is(err, io.ErrUnexpectedEOF) || output.Len() != 0 {
		t.Fatalf("reader failure was lost or emitted partial output: %v", err)
	}
	args[1] = "pk-ui.component.skiplink/default"
	if err := run(args, strings.NewReader("{}"), &output); err == nil || output.Len() != 0 {
		t.Fatalf("read-only helper accepted property edits or emitted partial output: %v", err)
	}
}
