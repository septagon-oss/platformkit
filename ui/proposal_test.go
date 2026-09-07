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
	"github.com/septagon-oss/platformkit/ui/css"
	g "maragu.dev/gomponents"
)

func proposalExport(t *testing.T, examples []c.Example) ui.DesignExport {
	t.Helper()
	out, err := ui.Export(design.Default(), examples)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPropsProposalChangesOneNestedOccurrence(t *testing.T) {
	button := c.ExampleOf(c.ExampleInfo{ID: "save/local", ComponentID: "button"}, c.ButtonProps{Label: "Save"}, c.Button)
	parent := func(id string) c.Example {
		return c.ExampleWithChildren(c.ExampleInfo{ID: id, ComponentID: "form"}, c.FormProps{}, []g.Node{button.Node}, c.Form)
	}
	examples := []c.Example{parent("second"), parent("first")}
	base := proposalExport(t, examples)
	proposal := ui.PropsProposal{BaseSHA256: base.SHA256, Path: []string{"first", "save/local"}, Props: json.RawMessage(`{"label":"Create & keep"}`)}
	before, _ := json.Marshal(proposal)
	candidate, projected, err := ui.ProjectProps(design.Default(), examples, proposal)
	if err != nil {
		t.Fatal(err)
	}
	if candidate[0].Node != examples[0].Node || candidate[1].Node == examples[1].Node || candidate[1].ID != "first" {
		t.Fatal("projection changed order or replaced the unrelated capture")
	}
	if !reflect.DeepEqual(base.Examples[1], projected.Examples[1]) || projected.SHA256 == base.SHA256 {
		t.Fatal("projection changed the shared sibling occurrence or retained the old revision")
	}
	child := projected.Examples[0].Children[0]
	if child.Description.ID != "save/local" || !strings.Contains(child.Description.HTML, "Create &amp; keep") || child.Span == nil {
		t.Fatal("projection lost exact local identity, escaping or observed ownership")
	}
	after, _ := json.Marshal(proposal)
	if string(before) != string(after) || !reflect.DeepEqual(base, proposalExport(t, examples)) {
		t.Fatal("projection mutated its request or authoritative examples")
	}
	if accepted, out, err := ui.ProjectProps(design.Default(), candidate, proposal); !errors.Is(err, ui.ErrStaleExport) || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
		t.Fatal("old revision accepted after the first change")
	}
	proposal.BaseSHA256 = projected.SHA256
	proposal.Props = json.RawMessage(`{"label":"Final"}`)
	slices.Reverse(candidate)
	if _, _, err := ui.ProjectProps(design.Default(), candidate, proposal); err != nil {
		t.Fatalf("fresh identity-addressed edit failed after root reordering: %v", err)
	}
}

func TestPropsProposalFreshnessIncludesPaletteCSSAndSource(t *testing.T) {
	example := c.ExampleOf(c.ExampleInfo{ID: "button", ComponentID: "button"}, c.ButtonProps{Label: "Save"}, c.Button)
	examples := []c.Example{example}
	base := proposalExport(t, examples)
	proposal := ui.PropsProposal{BaseSHA256: base.SHA256, Path: []string{"button"}, Props: json.RawMessage(`{"label":"Changed"}`)}
	for _, change := range []string{"palette", "css", "props", "schema", "metadata"} {
		t.Run(change, func(t *testing.T) {
			theme, inputs := design.Default(), slices.Clone(examples)
			var extra []ui.Extra
			switch change {
			case "palette":
				theme.Light.AccentDefault = "#123456"
			case "css":
				extra = []ui.Extra{{Sheets: []*css.Sheet{css.NewSheet().Select(".fixture", css.Decl("opacity", css.Literal("0.5")))}}}
			case "props":
				inputs[0], _ = example.WithProps(json.RawMessage(`{"label":"Elsewhere"}`))
			case "schema":
				inputs[0] = c.ExampleOf(example.ExampleInfo, c.TextProps{Content: "Save"}, c.Text)
			case "metadata":
				inputs[0].Name = "Renamed source example"
			}
			accepted, out, err := ui.ProjectProps(theme, inputs, proposal, extra...)
			if !errors.Is(err, ui.ErrStaleExport) || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
				t.Fatalf("%s change did not refuse the old revision: %v", change, err)
			}
		})
	}
}

func TestPropsProposalRefusesUnobservedAndRetainedOpaqueAliases(t *testing.T) {
	button := c.ExampleOf(c.ExampleInfo{ID: "action", ComponentID: "button"}, c.ButtonProps{Label: "Save"}, func(p c.ButtonProps) g.Node {
		if p.Label == "Fail" {
			return failedExportNode{}
		}
		return c.Button(p)
	})
	for _, mode := range []string{"unobserved", "unresolved", "opaque", "retained", "hidden-alias", "candidate-hidden", "render-failure"} {
		t.Run(mode, func(t *testing.T) {
			type slots struct {
				Body  g.Node
				Alias g.Node `json:"-"`
			}
			input := slots{Body: button.Node}
			if mode == "opaque" {
				input.Body = g.Group{button.Node}
			}
			if mode == "unresolved" {
				input.Body = nil
			}
			if mode == "hidden-alias" {
				input.Alias = button.Node
			}
			owner := c.ExampleWithSlots(c.ExampleInfo{ID: "owner", ComponentID: "form"}, c.FormProps{}, input, func(p c.FormProps, s slots) g.Node {
				switch mode {
				case "unobserved":
					return c.Form(p)
				case "retained", "unresolved":
					return c.Form(p, button.Node)
				case "hidden-alias":
					return c.Form(p, s.Alias)
				case "candidate-hidden":
					if strings.Contains(s.Body.(interface{ String() string }).String(), "Changed") {
						return c.Form(p)
					}
				}
				return c.Form(p, s.Body)
			})
			examples := []c.Example{owner}
			base := proposalExport(t, examples)
			patch := json.RawMessage(`{"label":"Changed"}`)
			if mode == "render-failure" {
				patch = json.RawMessage(`{"label":"Fail"}`)
			}
			proposal := ui.PropsProposal{BaseSHA256: base.SHA256, Path: []string{"owner", "action"}, Props: patch}
			accepted, out, err := ui.ProjectProps(design.Default(), examples, proposal)
			if err == nil || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
				t.Fatalf("unsafe projection was accepted: %v", err)
			}
			if !reflect.DeepEqual(base, proposalExport(t, examples)) {
				t.Fatal("refused proposal changed the original capture")
			}
		})
	}
}

func TestPropsProposalRenderPassesAndInvalidPaths(t *testing.T) {
	calls := 0
	example := c.ExampleOf(c.ExampleInfo{ID: "root", ComponentID: "text"}, c.TextProps{Content: "Before"}, func(p c.TextProps) g.Node {
		calls++
		return c.Text(p)
	})
	examples := []c.Example{example}
	base := proposalExport(t, examples)
	proposal := ui.PropsProposal{BaseSHA256: base.SHA256, Path: []string{"root"}, Props: json.RawMessage(`{"content":"After"}`)}
	calls = 0
	if _, _, err := ui.ProjectProps(design.Default(), examples, proposal); err != nil || calls != 2 {
		t.Fatalf("want two explicit export renders, got %d: %v", calls, err)
	}
	for _, path := range [][]string{nil, {}, {"missing"}, {"root", "missing"}, {"Root"}} {
		proposal.Path = path
		accepted, out, err := ui.ProjectProps(design.Default(), examples, proposal)
		if err == nil || accepted != nil || !reflect.DeepEqual(out, ui.DesignExport{}) {
			t.Fatalf("invalid path %q accepted: %v", path, err)
		}
	}
}

func TestPropsProposalJSONRefusalsPreserveReceiver(t *testing.T) {
	valid := `{"baseSHA256":"revision","path":["root"],"props":{"label":"Save"}}`
	for _, data := range []string{"null", "[]", "{}", valid + "{}",
		strings.Replace(valid, `"path":["root"]`, `"path":null`, 1),
		strings.Replace(valid, `"baseSHA256":"revision"`, `"baseSHA256":null`, 1),
		strings.Replace(valid, "baseSHA256", "BaseSHA256", 1),
		strings.Replace(valid, `"props":`, `"props":{},"props":`, 1),
		strings.Replace(valid, `"path":["root"]`, `"path":[1]`, 1)} {
		value := ui.PropsProposal{BaseSHA256: "unchanged", Path: []string{"original"}, Props: json.RawMessage(`{}`)}
		before, _ := json.Marshal(value)
		if err := value.UnmarshalJSON([]byte(data)); err == nil {
			t.Fatalf("accepted invalid proposal %s", data)
		}
		after, _ := json.Marshal(value)
		if string(before) != string(after) {
			t.Fatal("failed decoding mutated the receiver")
		}
	}
}
