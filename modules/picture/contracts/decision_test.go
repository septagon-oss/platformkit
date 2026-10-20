package contracts_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
)

type pure struct{}

func (pure) Declare(n, s string) (contracts.Template, error) { return contracts.Compile(n, s) }
func (pure) Render(t contracts.Template, v contracts.Value) (string, error) {
	r, err := contracts.Resolve(t, v)
	if err != nil {
		return "", err
	}
	return contracts.Compose(t, r)
}
func TestPureDecisions(t *testing.T) {
	picturetest.RunDecisions(t, func() picturetest.Decision { return pure{} })
}
func TestFakeDecisions(t *testing.T) {
	picturetest.RunDecisions(t, func() picturetest.Decision { return &picturetest.Fake{} })
}
func TestDeclarationsAndResolvedValuesAreDetached(t *testing.T) {
	template, err := contracts.Compile("fixture", picturetest.SVG)
	if err != nil {
		t.Fatal(err)
	}
	parts, slots := template.Parts(), template.Slots()
	parts[0].Options[0] = "changed"
	slots[0] = "changed"
	v := picturetest.Value()
	r, err := contracts.Resolve(template, v)
	if err != nil {
		t.Fatal(err)
	}
	v.Colours[0] = "#000000"
	v.Options[0].Option = "square"
	canonical := r.Canonical()
	canonical[0] = 'x'
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			body, err := contracts.Compose(template, r)
			if err != nil || body != picturetest.Oracle {
				t.Errorf("mutated or concurrent output %q %v", body, err)
			}
		})
	}
	wg.Wait()
	changed, err := contracts.Compile("same label", picturetest.SVG+" ")
	if err != nil {
		t.Fatal(err)
	}
	other, _ := contracts.Resolve(changed, picturetest.Value())
	if other.Identity() == r.Identity() {
		t.Fatal("template bytes absent from identity")
	}
	if body, err := contracts.Compose(changed, r); body != "" || err == nil {
		t.Fatal("cross-template resolved value accepted")
	}
	lower := picturetest.Value()
	lower.Colours[0] = strings.ToLower(lower.Colours[0])
	normalized, _ := contracts.Resolve(template, lower)
	if normalized.Identity() != r.Identity() {
		t.Fatal("hex case affected identity")
	}
	swapped := picturetest.Value()
	swapped.Colours[0], swapped.Colours[1] = swapped.Colours[1], swapped.Colours[0]
	reordered, _ := contracts.Resolve(template, swapped)
	if reordered.Identity() == r.Identity() {
		t.Fatal("slot order absent from identity")
	}
}

func TestGeometryGrammarRejectsInvalidShapes(t *testing.T) {
	root := func(child string) string {
		return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 80">` + child + `</svg>`
	}
	for _, child := range []string{
		`<path d="M0 0 L10 20 30 40 H5 V6 C1 2 3 4 5 6 S1 2 3 4 Q1 2 3 4 T1 2 A1 2 3 0 1 4 5 Z"/>`,
		`<g transform="matrix(1 0 0 1 0 0) translate(1, 2) scale(2) rotate(30 0 0) skewX(2) skewY(3)"><polygon points="0,0 10,0 10,10"/></g>`,
		`<rect width="0" height="0" rx="1"/><ellipse rx="0" ry="1"/><polyline points="0 0 1 1"/>`,
	} {
		template, err := contracts.Compile("fixture", root(child))
		if err != nil {
			t.Fatalf("valid geometry refused: %s: %v", child, err)
		}
		r, err := contracts.Resolve(template, contracts.Value{Background: "#ffffff"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := contracts.Compose(template, r); err != nil {
			t.Fatal(err)
		}
	}
	for _, child := range []string{
		`<path/>`, `<circle/>`, `<ellipse rx="1"/>`, `<rect width="1"/>`, `<polygon/>`,
		`<path d="L1 2"/>`, `<path d="M0 0 X1 2"/>`, `<path d="M0"/>`, `<path d="M0 0 1"/>`, `<path d="M0 0,"/>`, `<path d="M0 0,L1 2"/>`, `<path d="M0 0 Z1"/>`,
		`<path d="M0 0 A-1 1 0 0 1 2 3"/>`, `<path d="M0 0 A1 1 0 2 1 2 3"/>`, `<path d="M0 0 A1 1 0 0.0 1 2 3"/>`,
		`<polyline points="0 0"/>`, `<polygon points="0 0 1 1"/>`, `<polygon points="0 0 1 1 2"/>`,
		`<g transform="translate(1 2 3)"/>`, `<g transform="unknown(1)"/>`, `<g transform="translate(1) extra"/>`, `<g transform="scale(1)rotate(2)"/>`,
		`<g opacity="-1"/>`, `<g opacity="1.1"/>`, `<g fill-opacity="-1"/>`, `<g stroke-opacity="2"/>`, `<g stroke-width="8193"/>`, `<g stroke-width="-1"/>`, `<g stroke-miterlimit="0"/>`, `<g stroke-miterlimit="1001"/>`,
		`<g stroke-linecap="other"/>`, `<g stroke-linejoin="other"/>`, `<g fill-rule="other"/>`,
		`<rect width="-1" height="2"/>`, `<circle r="-1"/>`, `<ellipse rx="1" ry="-1"/>`,
	} {
		t.Run(child, func(t *testing.T) {
			template, err := contracts.Compile("fixture", root(child))
			if err == nil || template.Digest() != ([32]byte{}) {
				t.Fatalf("invalid geometry admitted: %s", child)
			}
		})
	}
	for _, box := range []string{"0 0 0 1", "0 0 1 -1", "0 0 8193 1", "0 0 1", "0 0 1 1 1", "0,,0,1,1", "0 0 1 1,"} {
		bad := strings.Replace(root(""), "0 0 100 80", box, 1)
		if _, err := contracts.Compile("fixture", bad); err == nil {
			t.Errorf("invalid viewBox admitted %q", box)
		}
	}
	for _, attr := range []string{`width="0"`, `height="8193"`, `width="10px"`, `preserveAspectRatio="xMinYMin"`} {
		bad := strings.Replace(root(""), "viewBox=", attr+" viewBox=", 1)
		if _, err := contracts.Compile("fixture", bad); err == nil {
			t.Errorf("invalid root attribute admitted %s", attr)
		}
	}
	if _, err := contracts.Compile(strings.Repeat("\"", 80), root("<"+strings.Repeat("x", 2000)+"/>")); err == nil || len(err.Error()) > 256 {
		t.Fatalf("unbounded diagnostic: %v", err)
	}
	for _, name := range []string{"", "bad\nname", strings.Repeat("a", 81)} {
		if _, err := contracts.Compile(name, root("")); err == nil {
			t.Errorf("invalid declaration label admitted %q", name)
		}
	}
	if _, err := contracts.Compile("fixture", root(strings.Repeat(`<polyline points="0 0 1 1 2 2 3 3 4 4"/>`, 1640))); err == nil || !strings.Contains(err.Error(), "16384") {
		t.Fatalf("numeric budget %v", err)
	}
}

func TestVocabularyBoundsAndMarkerNesting(t *testing.T) {
	root := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1">`
	for _, attr := range []string{`data-part="a"`, `data-option="one"`, `data-part="A" data-option="one"`, `data-part="a" data-option="a!"`, `data-slot=""`, `data-slot="stroke:"`, `data-slot="primary:other"`} {
		if _, err := contracts.Compile("fixture", root+"<g "+attr+"/></svg>"); err == nil {
			t.Errorf("invalid marker %s", attr)
		}
	}
	if _, err := contracts.Compile("fixture", root+`<g data-part="a" data-option="one"><g><g data-part="b" data-option="one"/></g></g></svg>`); err == nil {
		t.Fatal("nested marker admitted")
	}
	var parts, options, slots strings.Builder
	for i := range 65 {
		options.WriteString(fmt.Sprintf(`<g data-part="a" data-option="o%d"/>`, i))
		if i < 33 {
			parts.WriteString(fmt.Sprintf(`<g data-part="p%d" data-option="one"/>`, i))
		}
		if i < 17 {
			slots.WriteString(fmt.Sprintf(`<g data-slot="s%d"/>`, i))
		}
	}
	for _, body := range []string{parts.String(), options.String(), slots.String()} {
		if _, err := contracts.Compile("fixture", root+body+"</svg>"); err == nil {
			t.Fatal("vocabulary overflow admitted")
		}
	}
}
