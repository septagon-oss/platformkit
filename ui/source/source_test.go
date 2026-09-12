package source_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/source"
)

func TestProducerSourcePersistence(t *testing.T) {
	f := newProducerFixture(t)
	base := f.snapshot(t, nil)
	proposal := ui.PropsProposal{
		BaseSHA256: base.SHA256,
		Path:       []string{"screen/editor", "action/save"},
		Props:      json.RawMessage(`{"label":"Create & keep"}`),
	}

	t.Run("review and apply preserve the complete producer result", func(t *testing.T) {
		f.restoreAfter(t)
		expected := f.snapshot(t, &proposal)
		if expected.SHA256 == base.SHA256 {
			t.Fatal("fixture projection did not change the snapshot")
		}
		change, err := source.Prepare(t.Context(), f.producer, f.target("save :="), proposal)
		if err != nil {
			t.Fatal(err)
		}
		f.assertSource(t, f.program)
		review := change.Review()
		if review.File != f.file || review.BeforeSHA256 != fileDigest(f.program) ||
			review.AfterSHA256 != fileDigest([]byte(review.Source)) ||
			review.BaseSHA256 != base.SHA256 || review.ResultSHA256 != expected.SHA256 {
			t.Fatalf("review does not identify the source and snapshot revisions: %+v", review)
		}
		for _, preserved := range []string{"// Preserve the author's explanation.", "// Keep this inline note.", `AriaLabel: "Keep accessible action"`} {
			if !strings.Contains(review.Source, preserved) {
				t.Fatalf("review lost source content %q", preserved)
			}
		}
		reviewedSource := review.Source
		review.Source = "package main\n"
		review.AfterSHA256 = fileDigest([]byte(review.Source))
		if change.Review().Source != reviewedSource {
			t.Fatal("mutating a detached review changed the prepared source")
		}
		if err := change.Apply(t.Context()); err != nil {
			t.Fatal(err)
		}
		f.assertSource(t, []byte(reviewedSource))
		info, err := os.Stat(f.file)
		if err != nil || info.Mode().Perm() != 0o640 {
			t.Fatalf("source mode was not retained: %v, %v", info, err)
		}
		if rebuilt := f.snapshot(t, nil); !reflect.DeepEqual(rebuilt, expected) {
			t.Fatal("fresh build differs from the independently projected complete snapshot")
		}
		if err := change.Apply(t.Context()); err == nil {
			t.Fatal("the same reviewed change applied twice")
		}
		f.assertSource(t, []byte(reviewedSource))
	})

	t.Run("stale byte digest includes comments", func(t *testing.T) {
		f.restoreAfter(t)
		target := f.target("save :=")
		changed := append(bytes.Clone(f.program), []byte("\n// Edited after selecting this source.\n")...)
		writeFixtureFile(t, f.file, changed)
		if fresh := f.snapshot(t, nil); !reflect.DeepEqual(fresh, base) {
			t.Fatal("comment-only fixture change altered the semantic baseline")
		}
		change, err := source.Prepare(t.Context(), f.producer, target, proposal)
		if err == nil || change != nil {
			t.Fatal("stale source bytes produced a review")
		}
		f.assertSource(t, changed)
	})

	t.Run("wrong typed invocation with identical props is refused", func(t *testing.T) {
		change, err := source.Prepare(t.Context(), f.producer, f.target("decoy :="), proposal)
		if err == nil || change != nil {
			t.Fatal("editing a different typed capture passed full snapshot comparison")
		}
		f.assertSource(t, f.program)
	})

	for _, which := range []string{"target source", "other build input", "new build file", "assembly header", "module metadata"} {
		t.Run("apply refuses changed "+which, func(t *testing.T) {
			f.restoreAfter(t)
			filename, original := f.file, f.program
			if which == "other build input" {
				filename, original = f.supportFile, f.support
			}
			if which == "assembly header" {
				filename, original = filepath.Join(f.producer.Dir, "fixture.h"), []byte("#define FIXTURE_VALUE 1\n")
				assembly := filepath.Join(f.producer.Dir, "empty.s")
				writeFixtureFile(t, filename, original)
				writeFixtureFile(t, assembly, []byte("#include \"fixture.h\"\n"))
				t.Cleanup(func() {
					for _, path := range []string{filename, assembly} {
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
			if which == "module metadata" {
				filename, original = filepath.Join(f.producer.Dir, "go.mod"), f.module
				t.Cleanup(func() { writeFixtureFile(t, filename, original) })
			}
			if which == "new build file" {
				filename, original = filepath.Join(f.producer.Dir, "added.go"), []byte("package main\n")
				t.Cleanup(func() {
					if err := os.Remove(filename); err != nil {
						t.Fatal(err)
					}
				})
			}
			change, err := source.Prepare(t.Context(), f.producer, f.target("save :="), proposal)
			if err != nil {
				t.Fatal(err)
			}
			f.assertSource(t, f.program)
			changed := append(bytes.Clone(original), []byte("\n// An independent source edit.\n")...)
			writeFixtureFile(t, filename, changed)
			if err := change.Apply(t.Context()); err == nil {
				t.Fatal("source mutation after review was overwritten or ignored")
			}
			if got := readFixtureFile(t, filename); !bytes.Equal(got, changed) {
				t.Fatal("refusal changed the independent edit")
			}
			retained := f
			if which == "module metadata" {
				retained.module = changed
			}
			if which != "target source" {
				retained.assertSource(t, f.program)
			}
			retained.assertModuleInputs(t)
		})
	}

	t.Run("apply retains a newer permission change", func(t *testing.T) {
		change, err := source.Prepare(t.Context(), f.producer, f.target("save :="), proposal)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(f.file, 0o640); err != nil {
				t.Fatal(err)
			}
		})
		if err := os.Chmod(f.file, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := change.Apply(t.Context()); err == nil {
			t.Fatal("a review applied after the source permissions changed")
		}
		info, err := os.Stat(f.file)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("refusal overwrote the newer source permissions: %v, %v", info, err)
		}
		f.assertSource(t, f.program)
	})

	t.Run("symlink target is refused", func(t *testing.T) {
		link := filepath.Join(f.producer.Dir, "links", "producer.go")
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(f.file, link); err != nil {
			t.Fatal(err)
		}
		target := f.target("save :=")
		target.File = link
		change, err := source.Prepare(t.Context(), f.producer, target, proposal)
		if err == nil || change != nil {
			t.Fatal("symlink target produced an applicable review")
		}
		if destination, err := os.Readlink(link); err != nil || destination != f.file {
			t.Fatalf("refusal replaced the symlink: %q, %v", destination, err)
		}
		f.assertSource(t, f.program)
	})

	t.Run("source outside producer module is refused", func(t *testing.T) {
		outside := filepath.Join(t.TempDir(), "producer.go")
		writeFixtureFile(t, outside, f.program)
		target := f.target("save :=")
		target.File = outside
		change, err := source.Prepare(t.Context(), f.producer, target, proposal)
		if err == nil || change != nil {
			t.Fatal("unowned external file produced an applicable review")
		}
		if got := readFixtureFile(t, outside); !bytes.Equal(got, f.program) {
			t.Fatal("refusal changed the external file")
		}
		f.assertSource(t, f.program)
	})

	for _, stage := range []string{"baseline", "proposal", "rebuilt"} {
		t.Run("failed "+stage+" producer writes nothing", func(t *testing.T) {
			producer := f.producer
			producer.Args = append([]string{}, f.producer.Args...)
			producer.Args = append(producer.Args, "--fail="+stage)
			change, err := source.Prepare(t.Context(), producer, f.target("save :="), proposal)
			if err == nil || change != nil {
				t.Fatal("failed producer returned a review")
			}
			f.assertSource(t, f.program)
			if got := readFixtureFile(t, f.supportFile); !bytes.Equal(got, f.support) {
				t.Fatal("failed producer changed another source file")
			}
		})
	}
}

type producerFixture struct {
	producer          source.GoProducer
	file, supportFile string
	program, support  []byte
	module, sums      []byte
}

func newProducerFixture(t *testing.T) producerFixture {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	program, err := format.Source([]byte(producerProgram))
	if err != nil {
		t.Fatal(err)
	}
	f := producerFixture{
		producer: source.GoProducer{Dir: dir, Package: ".", Args: []string{"--suite=fixture"}},
		file:     filepath.Join(dir, "producer.go"), supportFile: filepath.Join(dir, "support.go"),
		program: program, support: []byte("package main\n\nconst independentCopy = \"Untouched sibling\"\n"),
	}
	writeFixtureFile(t, f.file, f.program)
	writeFixtureFile(t, f.supportFile, f.support)
	module := fmt.Sprintf("module example.test/source-producer\n\ngo 1.26.6\n\nrequire github.com/septagon-oss/platformkit v0.0.0\n\nreplace github.com/septagon-oss/platformkit => %q\n", root)
	writeFixtureFile(t, filepath.Join(dir, "go.mod"), []byte(module))
	writeFixtureFile(t, filepath.Join(dir, "go.sum"), readFixtureFile(t, filepath.Join(root, "go.sum")))
	f.runGo(t, nil, "mod", "tidy")
	f.module = readFixtureFile(t, filepath.Join(dir, "go.mod"))
	f.sums = readFixtureFile(t, filepath.Join(dir, "go.sum"))
	return f
}

func (f producerFixture) restoreAfter(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		writeFixtureFile(t, f.file, f.program)
		writeFixtureFile(t, f.supportFile, f.support)
	})
}

func (f producerFixture) target(marker string) source.Target {
	prefix, _, _ := strings.Cut(string(f.program), marker)
	line := strings.Count(prefix, "\n") + 1
	return source.Target{File: f.file, Line: line, SHA256: fileDigest(f.program)}
}

func (f producerFixture) snapshot(t *testing.T, proposal *ui.PropsProposal) ui.DesignExport {
	t.Helper()
	args := append([]string{"run", "-mod=readonly", "."}, f.producer.Args...)
	var input []byte
	if proposal != nil {
		var err error
		input, err = json.Marshal(proposal)
		if err != nil {
			t.Fatal(err)
		}
		args = append(args, "--proposal")
	}
	var snapshot ui.DesignExport
	if err := json.Unmarshal(f.runGo(t, input, args...), &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func (f producerFixture) runGo(t *testing.T, input []byte, args ...string) []byte {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = f.producer.Dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=", "CGO_ENABLED=0")
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("fixture go %v: %v\n%s", args, err, &stderr)
	}
	return output
}

func (f producerFixture) assertSource(t *testing.T, want []byte) {
	t.Helper()
	if got := readFixtureFile(t, f.file); !bytes.Equal(got, want) {
		t.Fatal("source file differs from the expected bytes")
	}
	f.assertModuleInputs(t)
}

func (f producerFixture) assertModuleInputs(t *testing.T) {
	t.Helper()
	for filename, want := range map[string][]byte{"go.mod": f.module, "go.sum": f.sums} {
		if got := readFixtureFile(t, filepath.Join(f.producer.Dir, filename)); !bytes.Equal(got, want) {
			t.Fatalf("source persistence changed producer %s", filename)
		}
	}
}

func writeFixtureFile(t *testing.T, filename string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filename, data, 0o640); err != nil {
		t.Fatal(err)
	}
}

func readFixtureFile(t *testing.T, filename string) []byte {
	t.Helper()
	data, err := os.ReadFile(filename)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func fileDigest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

const producerProgram = `package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func captureInfo(section, name, component string) c.ExampleInfo {
	return c.ExampleInfo{ID: section + "/" + name, ComponentID: component}
}

func examples() []c.Example {
	// Preserve the author's explanation.
	save := c.ExampleOf(captureInfo("action", "save", "button"), c.ButtonProps{
		Label: "Save", // Keep this inline note.
		AriaLabel: "Keep accessible action",
	}, c.Button)
	decoy := c.ExampleOf(captureInfo("action", "other", "button"), c.ButtonProps{
		Label: "Save",
		AriaLabel: "Keep accessible action",
	}, c.Button)
	form := c.ExampleWithChildren(captureInfo("screen", "editor", "form"), c.FormProps{}, []g.Node{save.Node, decoy.Node}, c.Form)
	text := c.ExampleOf(captureInfo("screen", "sibling", "text"), c.TextProps{Content: independentCopy}, c.Text)
	return []c.Example{form, text}
}

func main() {
	suite := flag.String("suite", "", "fixture identity")
	proposal := flag.Bool("proposal", false, "project stdin proposal")
	failure := flag.String("fail", "", "intentional failure stage")
	flag.Parse()
	if *suite != "fixture" || *failure == "baseline" || (*failure == "proposal" && *proposal) {
		fmt.Fprintln(os.Stderr, "intentional fixture failure")
		os.Exit(1)
	}
	var snapshot ui.DesignExport
	var err error
	if *proposal {
		var request ui.PropsProposal
		err = json.NewDecoder(os.Stdin).Decode(&request)
		if err == nil {
			_, snapshot, err = ui.ProjectProps(design.Default(), examples(), request)
		}
	} else {
		snapshot, err = ui.Export(design.Default(), examples())
	}
	if err == nil && *failure == "rebuilt" && !*proposal && strings.Contains(snapshot.Examples[0].HTML, "Create &amp; keep") {
		err = fmt.Errorf("intentional rebuilt producer failure")
	}
	if err == nil {
		err = json.NewEncoder(os.Stdout).Encode(snapshot)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`
