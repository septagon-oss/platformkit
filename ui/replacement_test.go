package ui_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/css"
	g "maragu.dev/gomponents"
)

func TestReplacementProposalUsesNestedSourceAndDestination(t *testing.T) {
	calls := 0
	button := func(id, label string) examples.Example {
		return examples.ExampleOf(examples.ExampleInfo{ID: id, ComponentID: "button"}, c.ButtonProps{Label: label}, func(p c.ButtonProps) g.Node {
			calls++
			return c.Button(p)
		})
	}
	owner := func(id string, child examples.Example) examples.Example {
		return examples.ExampleWithChildren(examples.ExampleInfo{ID: id, ComponentID: "form"}, c.FormProps{}, []g.Node{child.Node}, c.Form)
	}
	target := button("save/✓", "Before")
	captures := []examples.Example{owner("source", button("choice/local", "Create & keep")), owner("second", target), owner("first", target)}
	base := proposalExport(t, captures)
	proposal := ui.ReplacementProposal{BaseSHA256: base.SHA256, Path: []string{"first", "save/✓"}, ReplacementPath: []string{"source", "choice/local"}}
	request, _ := json.Marshal(proposal)
	calls = 0
	candidate, projected, err := ui.ProjectReplacement(design.Default(), captures, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 6 {
		t.Fatalf("want two exports of three button occurrences, got %d calls", calls)
	}
	if candidate[0].Node != captures[0].Node || candidate[1].Node != captures[1].Node || candidate[2].Node == captures[2].Node {
		t.Fatal("replacement changed the source, a sibling occurrence or root order")
	}
	child := projected.Examples[0].Children[0]
	if child.Description.ID != "save/✓" || child.Description.ComponentID != "button" || child.Span == nil || !strings.Contains(child.Description.HTML, "Create &amp; keep") {
		t.Fatal("replacement lost target identity, escaping or ownership")
	}
	if !reflect.DeepEqual(base.Examples[1:], projected.Examples[1:]) || base.SHA256 == projected.SHA256 {
		t.Fatal("replacement changed unrelated exports or retained the old revision")
	}
	after, _ := json.Marshal(proposal)
	if string(request) != string(after) || !reflect.DeepEqual(base, proposalExport(t, captures)) {
		t.Fatal("replacement mutated inputs")
	}
	accepted, out, err := ui.ProjectReplacement(design.Default(), candidate, proposal)
	if !errors.Is(err, ui.ErrStaleExport) || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
		t.Fatal("old revision accepted")
	}
	proposal.BaseSHA256 = projected.SHA256
	slices.Reverse(candidate)
	_, unchanged, err := ui.ProjectReplacement(design.Default(), candidate, proposal)
	if err != nil || unchanged.SHA256 != projected.SHA256 {
		t.Fatal("reordered roots or same-content replacement failed")
	}
}

func TestReplacementProposalFreshnessIncludesSourceThemeAndCSS(t *testing.T) {
	makeText := func(id, value string) examples.Example {
		return examples.ExampleOf(examples.ExampleInfo{ID: id, ComponentID: "text"}, c.TextProps{Content: value}, c.Text)
	}
	captures := []examples.Example{makeText("target", "Old"), makeText("source", "New")}
	proposal := ui.ReplacementProposal{BaseSHA256: proposalExport(t, captures).SHA256, Path: []string{"target"}, ReplacementPath: []string{"source"}}
	for _, change := range []string{"source", "theme", "css", "metadata"} {
		t.Run(change, func(t *testing.T) {
			inputs, theme := slices.Clone(captures), design.Default()
			var extra []ui.Extra
			switch change {
			case "source":
				inputs[1] = makeText("source", "Revised")
			case "theme":
				theme.Light.AccentDefault = "#123456"
			case "css":
				extra = []ui.Extra{{Sheets: []*css.Sheet{css.NewSheet().Select(".fixture", css.Decl("opacity", css.Literal("0.5")))}}}
			case "metadata":
				inputs[1].Name = "Renamed"
			}
			accepted, out, err := ui.ProjectReplacement(theme, inputs, proposal, extra...)
			if !errors.Is(err, ui.ErrStaleExport) || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
				t.Fatalf("%s accepted stale base: %v", change, err)
			}
		})
	}
}

func TestReplacementProposalRefusesUnsafeOwnershipOnEitherPath(t *testing.T) {
	makeText := func(id, label string) examples.Example {
		return examples.ExampleOf(examples.ExampleInfo{ID: id, ComponentID: "text"}, c.TextProps{Content: label}, c.Text)
	}
	for _, side := range []string{"target", "source"} {
		for _, mode := range []string{"unobserved", "unresolved", "opaque", "unsupported", "retained", "candidate-hidden", "render-failure"} {
			t.Run(side+"/"+mode, func(t *testing.T) {
				child := makeText("child", "Old")
				type slots struct {
					Body     g.Node
					Callback func() g.Node
				}
				input := slots{Body: child.Node}
				if mode == "opaque" {
					input.Body = g.Group{child.Node}
				}
				if mode == "unresolved" {
					input.Body = nil
				}
				if mode == "unsupported" {
					input.Callback = func() g.Node { return child.Node }
				}
				owner := examples.ExampleWithSlots(examples.ExampleInfo{ID: "owner", ComponentID: "owner"}, c.FormProps{}, input, func(p c.FormProps, s slots) g.Node {
					switch mode {
					case "unobserved":
						return c.Form(p)
					case "unresolved", "retained":
						return c.Form(p, child.Node)
					case "candidate-hidden", "render-failure":
						if strings.Contains(s.Body.(interface{ String() string }).String(), "New") {
							if mode == "render-failure" {
								return failedExportNode{}
							}
							return c.Form(p)
						}
					}
					return c.Form(p, s.Body)
				})
				captures := []examples.Example{owner, makeText("other", "New")}
				base := proposalExport(t, captures)
				proposal := ui.ReplacementProposal{BaseSHA256: base.SHA256, Path: []string{"owner", "child"}, ReplacementPath: []string{"other"}}
				if side == "source" {
					proposal.Path, proposal.ReplacementPath = proposal.ReplacementPath, proposal.Path
				}
				accepted, out, err := ui.ProjectReplacement(design.Default(), captures, proposal)
				// Retained or conditional rendering in an untouched source owner is
				// safe: lookup reads the currently declared and observed capture.
				if side == "source" && (mode == "retained" || mode == "candidate-hidden" || mode == "render-failure") {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
					t.Fatalf("unsafe %s accepted: %v", mode, err)
				}
				if !reflect.DeepEqual(base, proposalExport(t, captures)) {
					t.Fatal("refusal mutated a source tree")
				}
			})
		}
	}
}

func TestReplacementProposalRefusesMissingPathsAndIncompatibleInterfaces(t *testing.T) {
	text := examples.ExampleOf(examples.ExampleInfo{ID: "text", ComponentID: "text"}, c.TextProps{Content: "Text"}, c.Text)
	button := examples.ExampleOf(examples.ExampleInfo{ID: "button", ComponentID: "button"}, c.ButtonProps{Label: "Button"}, c.Button)
	captures := []examples.Example{text, button}
	base := proposalExport(t, captures)
	for _, paths := range [][][]string{{{"text"}, {"button"}}, {nil, {"text"}}, {{"text"}, nil}, {{"missing"}, {"text"}}, {{"text"}, {"missing"}}, {{"text"}, {"text", "missing"}}} {
		accepted, out, err := ui.ProjectReplacement(design.Default(), captures, ui.ReplacementProposal{BaseSHA256: base.SHA256, Path: paths[0], ReplacementPath: paths[1]})
		if err == nil || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
			t.Fatalf("invalid paths or interface accepted: %v", err)
		}
	}
	// Equal claimed component names cannot disguise different actual schemas.
	button = examples.ExampleOf(examples.ExampleInfo{ID: "button", ComponentID: "text"}, c.ButtonProps{}, c.Button)
	if accepted, out, err := ui.ProjectReplacement(design.Default(), []examples.Example{text, button}, ui.ReplacementProposal{BaseSHA256: base.SHA256, Path: []string{"text"}, ReplacementPath: []string{"button"}}); err == nil || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
		t.Fatal("conflicting source contracts accepted")
	}
}

func TestReplacementProposalOverlappingPathsReadTheBaseSnapshot(t *testing.T) {
	makeOwner := func(id string, children ...g.Node) examples.Example {
		return examples.ExampleWithChildren(examples.ExampleInfo{ID: id, ComponentID: "form"}, c.FormProps{}, children, c.Form)
	}
	root := makeOwner("root", makeOwner("child").Node)
	captures := []examples.Example{root}
	base := proposalExport(t, captures)
	for _, paths := range [][][]string{{{"root"}, {"root"}}, {{"root"}, {"root", "child"}}, {{"root", "child"}, {"root"}}} {
		candidate, _, err := ui.ProjectReplacement(design.Default(), captures, ui.ReplacementProposal{BaseSHA256: base.SHA256, Path: paths[0], ReplacementPath: paths[1]})
		if err != nil {
			t.Fatalf("immutable overlap failed: %v", err)
		}
		changed, err := candidate[0].At(paths[0])
		if err != nil {
			t.Fatal(err)
		}
		description, err := changed.Describe()
		wantChildren := 1
		if len(paths[1]) == 2 {
			wantChildren = 0
		}
		if err != nil || description.ID != paths[0][len(paths[0])-1] || len(description.Children) != wantChildren {
			t.Fatal("overlap was not a finite copy of base inputs")
		}
		if !reflect.DeepEqual(base, proposalExport(t, captures)) {
			t.Fatal("overlapping replacement mutated source")
		}
	}
}

func TestReplacementProposalJSONRefusalsPreserveReceiver(t *testing.T) {
	valid := `{"baseSHA256":"revision","path":["root"],"replacementPath":["source","child/✓"]}`
	var decoded ui.ReplacementProposal
	if err := json.Unmarshal([]byte(valid), &decoded); err != nil || !slices.Equal(decoded.ReplacementPath, []string{"source", "child/✓"}) {
		t.Fatal("valid source proposal refused")
	}
	for _, data := range []string{"null", "[]", "{}", valid + "{}",
		strings.Replace(valid, `"path":["root"]`, `"path":null`, 1),
		strings.Replace(valid, `"replacementPath":["source","child/✓"]`, `"replacementPath":[]`, 1),
		strings.Replace(valid, `"baseSHA256":"revision"`, `"baseSHA256":null`, 1),
		strings.Replace(valid, "replacementPath", "ReplacementPath", 1),
		strings.Replace(valid, `"path":`, `"path":[],"path":`, 1),
		strings.Replace(valid, `"path":["root"]`, `"path":[1]`, 1),
		strings.Replace(valid, `"replacementPath":`, `"nativeId":"123","replacementPath":`, 1)} {
		value := ui.ReplacementProposal{BaseSHA256: "unchanged", Path: []string{"original"}, ReplacementPath: []string{"unchanged"}}
		before, _ := json.Marshal(value)
		if err := value.UnmarshalJSON([]byte(data)); err == nil {
			t.Fatalf("accepted %s", data)
		}
		after, _ := json.Marshal(value)
		if string(before) != string(after) {
			t.Fatal("refused JSON mutated receiver")
		}
	}
}
