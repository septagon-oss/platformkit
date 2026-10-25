package view_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
	"github.com/septagon-oss/platformkit/kit/events"
)

// TestThePackageImportsNothingAboveThePresentationContract is the boundary in the
// form that can say it in words: the *direct* imports of this package, listed by
// the toolchain, are these and no others. scripts/check_packages.sh asserts the
// same claim over the transitive closure, and cannot make it in this form, because
// the closure of everything here carries a database driver's value types through
// the uuid package the entity types themselves use.
func TestThePackageImportsNothingAboveThePresentationContract(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	allowed := []string{
		"bytes", "cmp", "encoding/json", "fmt", "math", "slices", "strconv", "strings", "time",
		"github.com/septagon-oss/platformkit/kit/entity",
		"github.com/septagon-oss/platformkit/kit/entity/display",
		"github.com/septagon-oss/platformkit/kit/entity/present",
	}
	for _, imp := range strings.Fields(string(out)) {
		if !slices.Contains(allowed, imp) {
			t.Errorf("%s imports %s, which is above the presentation contract", t.Name(), imp)
		}
	}
}

// schemaFile is the published document: its name carries the revision, so a shell
// pins one and never reads another.
const schemaFile = "page-v1.schema.json"

// messageKey is a copy key of somebody's seam — "screens.new", "present.true" —
// which is how a package starts holding words it never declared as its own.
var messageKey = regexp.MustCompile(`"(screens|present|resource|rest)\.[a-zA-Z]`)

// TestThePageContractIntroducesNoCopy is why every word on a Page is either the
// policy's or the caller's: a package that quotes a copy key owns that copy, and
// the completeness gate for every language would gain words nobody briefed.
func TestThePageContractIntroducesNoCopy(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no sources to read: %v", err)
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if found := messageKey.Find(raw); found != nil {
			t.Errorf("%s quotes the copy key %s: the Page introduces no words of its own", file, found)
		}
	}
}

// TestTheGeneratedPageSchemaIsWhatTheTypesProject keeps the published document
// honest about the Go types: a member edited without the writer being run fails a
// normal test rather than reaching a shell that builds against a stale shape.
func TestTheGeneratedPageSchemaIsWhatTheTypesProject(t *testing.T) {
	t.Parallel()
	raw, err := json.MarshalIndent(events.SchemaOf(reflect.TypeFor[view.Page]()), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(schemaFile)
	if err != nil {
		t.Fatalf("the published schema is not there to compare: %v", err)
	}
	if string(published) != string(append(raw, '\n')) {
		t.Errorf("%s is not what the types project: run `make generate`", schemaFile)
	}
}

func TestAPublishedPageValidatesAgainstTheSchema(t *testing.T) {
	t.Parallel()
	schema := events.SchemaOf(reflect.TypeFor[view.Page]())
	for _, kind := range []view.Kind{view.KindList, view.KindRecord, view.KindForm} {
		in := listInput()
		in.State.Kind = kind
		if kind == view.KindForm {
			in.State.Form = view.FormState{Create: true, Title: "New task"}
		}
		if err := schema.Validate(bytes(t, build(t, in))); err != nil {
			t.Errorf("%s page: %v", kind, err)
		}
	}
	if err := schema.Validate([]byte(`{"kind":"list"}`)); err == nil {
		t.Error("a page missing every other member validated: no member of a Page is optional")
	}
}

func TestAPageWrittenAtAnotherVersionIsNotSupported(t *testing.T) {
	t.Parallel()
	page := build(t, listInput())
	if page.PageVersion != view.PageVersion || !view.SupportedVersion(page.PageVersion) {
		t.Fatalf("a build wrote a page it does not read: %+v", page)
	}
}

func zeroTime() time.Time { return time.Time{} }

// wroteNothing reports the zero Page: a refusal writes no page, so every member of
// the result is still the zero value it was handed back as.
func wroteNothing(page view.Page) bool { return reflect.DeepEqual(page, view.Page{}) }
