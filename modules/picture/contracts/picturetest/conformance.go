package picturetest

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

// SVG is a domain-free fixture with one option-bearing part and two paint slots.
const SVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 80" width="100" height="80">
  <circle data-part="shape" data-option="round" data-slot="primary" cx="25" cy="30" r="12" fill="#010203" stroke="#202020"/>
  <rect data-part="shape" data-option="square" data-slot="primary" x="10" y="15" width="30" height="30" fill="#010203"/>
  <line data-slot="stroke:accent" x1="15" y1="65" x2="85" y2="65" stroke="#010203" stroke-width="2"/>
</svg>`

// Oracle is independently authored expected XML, including sorted attributes.
const Oracle = `<svg height="80" preserveAspectRatio="xMidYMid meet" viewBox="0 0 100 80" width="100" xmlns="http://www.w3.org/2000/svg"><rect fill="#FFFFFF" height="80" width="100" x="0" y="0"></rect><circle cx="25" cy="30" fill="#AABBCC" r="12" stroke="#202020"></circle><line stroke="#112233" stroke-width="2" x1="15" x2="85" y1="65" y2="65"></line></svg>`

// Value returns a fresh complete baseline value.
func Value() contracts.Value {
	return contracts.Value{Options: []contracts.Selection{{Part: "shape", Option: "round"}}, Colours: []string{"#AABBCC", "#112233"}, Background: "#FFFFFF"}
}

// Decision is the shared declaration/render surface implemented by the fake and pure adapter.
type Decision interface {
	Declare(string, string) (contracts.Template, error)
	Render(contracts.Template, contracts.Value) (string, error)
}

// RunDecisions checks independent outputs and refusals against any decision adapter.
func RunDecisions(t *testing.T, factory func() Decision) {
	t.Helper()
	t.Run("literal output and identities", func(t *testing.T) {
		d := factory()
		template, err := d.Declare("fixture", SVG)
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprintf("%x", template.Digest()); got != "1fdaecd0f607fabc10659e15b9d0e00fcf97ffa4e465961d55e6ab2639f31293" {
			t.Fatalf("template digest %s", got)
		}
		r, err := contracts.Resolve(template, Value())
		if err != nil {
			t.Fatal(err)
		}
		if string(r.Canonical()) != `{"options":[{"part":"shape","option":"round"}],"colours":["#AABBCC","#112233"],"background":"#FFFFFF"}` {
			t.Fatalf("canonical %s", r.Canonical())
		}
		if got := fmt.Sprintf("%x", r.Identity()); got != "e3bf5fc3f758a23386b85528b5c640c1d949741b36fa69702377ee35fee56fe5" {
			t.Fatalf("identity %s", got)
		}
		for range 3 {
			body, err := d.Render(template, Value())
			if err != nil || body != Oracle {
				t.Fatalf("render %q: %v", body, err)
			}
			if fmt.Sprintf("%x", sha256.Sum256([]byte(body))) != "2ab9f69bb3f58230f64771b7fdb0a55cee88c9536bde570a2546fd4be6e63fb1" {
				t.Fatal("body digest")
			}
		}
	})
	t.Run("closed element allowlist", func(t *testing.T) {
		for _, element := range strings.Fields("script foreignObject style image use a animate animateTransform set filter feImage text tspan font linearGradient radialGradient pattern defs clipPath mask svg unknown") {
			t.Run(element, func(t *testing.T) {
				source := strings.Replace(SVG, "</svg>", "<"+element+"/></svg>", 1)
				badTemplate(t, factory(), source, element)
			})
		}
	})
	t.Run("closed attribute allowlist", func(t *testing.T) {
		for _, attr := range strings.Fields("onload onclick ONLOAD style class id data-other href src stroke-dasharray filter clip-path xlink:href") {
			t.Run(attr, func(t *testing.T) { badTemplate(t, factory(), strings.Replace(SVG, "cx=", attr+`="x" cx=`, 1), attr) })
		}
	})
	t.Run("unsafe paint", func(t *testing.T) {
		for _, property := range []string{"fill", "stroke"} {
			for _, value := range []string{"red", "url(https://invalid.example/x)", "url(#local)", "data:image/svg+xml,x", "var(--x)", "currentColor", "inherit", "#abc", "#11223344"} {
				t.Run(property+value, func(t *testing.T) {
					badTemplate(t, factory(), strings.Replace(SVG, property+`="#010203"`, property+`="`+value+`"`, 1), property)
				})
			}
		}
	})
	cases := []struct{ name, source, detail string }{
		{"no namespace", strings.Replace(SVG, ` xmlns="http://www.w3.org/2000/svg"`, "", 1), "namespace"},
		{"wrong namespace", strings.Replace(SVG, "http://www.w3.org/2000/svg", "urn:other", 1), "namespace"},
		{"child namespace", strings.Replace(SVG, "<circle", `<circle xmlns=""`, 1), "namespace"},
		{"namespace prefix", strings.Replace(SVG, "<circle", `<s:circle`, 1), "circle"},
		{"duplicate attribute", strings.Replace(SVG, "cx=", `fill="#000000" cx=`, 1), "duplicate"},
		{"DTD", `<!DOCTYPE svg>` + SVG, "directive"},
		{"external entity", `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]>` + SVG, "directive"},
		{"recursive entity", `<!DOCTYPE svg [<!ENTITY x "&x;">]>` + SVG, "directive"},
		{"unknown entity", strings.Replace(SVG, "#010203", "&unknown;", 1), "XML"},
		{"stylesheet", `<?xml-stylesheet href="x"?>` + SVG, "processing instruction"},
		{"XML 1.1", `<?xml version="1.1"?>` + SVG, "processing instruction"},
		{"standalone no", `<?xml version="1.0" standalone="no"?>` + SVG, "processing instruction"},
		{"BOM", "\ufeff" + SVG, "UTF-8"},
		{"control in comment", strings.Replace(SVG, "</svg>", "<!--\x01--></svg>", 1), "XML"},
		{"noncharacter in comment", strings.Replace(SVG, "</svg>", "<!--\uffff--></svg>", 1), "XML"}, {"invalid UTF8", "\xff" + SVG, "UTF-8"},
		{"text", strings.Replace(SVG, "</svg>", "hello</svg>", 1), "character data"},
		{"CDATA", strings.Replace(SVG, "</svg>", "<![CDATA[hello]]></svg>", 1), "character data"},
		{"unclosed", strings.TrimSuffix(SVG, "</svg>"), "complete"},
		{"two roots", SVG + SVG, "root"},
		{"too large", strings.Repeat(" ", 262145), "262144"},
		{"too deep", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1">` + strings.Repeat("<g>", 32) + strings.Repeat("</g>", 32) + "</svg>", "depth"},
		{"too many elements", strings.Replace(SVG, "</svg>", strings.Repeat("<g/>", 2048)+"</svg>", 1), "2048"},
		{"long attribute", strings.Replace(SVG, "<circle", `<circle transform="`+strings.Repeat(" ", 65537)+`"`, 1), "65536"},
		{"missing viewBox", strings.Replace(SVG, ` viewBox="0 0 100 80"`, "", 1), "viewBox"},
		{"paired markers", strings.Replace(SVG, ` data-option="round"`, "", 1), "together"},
		{"invalid part", strings.Replace(SVG, `data-part="shape"`, `data-part="Shape"`, 1), "data-part"},
		{"invalid slot", strings.Replace(SVG, `data-slot="primary"`, `data-slot="fill:primary"`, 1), "data-slot"},
		{"nested options", strings.Replace(SVG, "<circle", `<g data-part="outer" data-option="one"><circle`, 1), "nested"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { badTemplate(t, factory(), c.source, c.detail) })
	}
	t.Run("invalid numeric geometry", func(t *testing.T) {
		for _, v := range []string{"NaN", "Inf", "1e309", "1000001", "1%", "1px", "1,", "1-2", "-1"} {
			t.Run(v, func(t *testing.T) { badTemplate(t, factory(), strings.Replace(SVG, `r="12"`, `r="`+v+`"`, 1), "r") })
		}
	})
	t.Run("invalid values", func(t *testing.T) {
		template, err := factory().Declare("fixture", SVG)
		if err != nil {
			t.Fatal(err)
		}
		for _, mutate := range []func(*contracts.Value){func(v *contracts.Value) { v.Options = nil }, func(v *contracts.Value) { v.Options[0].Option = "unknown" }, func(v *contracts.Value) { v.Options[0].Part = "unknown" }, func(v *contracts.Value) { v.Options = append(v.Options, v.Options[0]) }, func(v *contracts.Value) { v.Colours = nil }, func(v *contracts.Value) { v.Colours[0] = "red" }, func(v *contracts.Value) { v.Background = "" }, func(v *contracts.Value) { v.Background = " #FFFFFF" }} {
			v := Value()
			mutate(&v)
			if body, err := factory().Render(template, v); err == nil || body != "" {
				t.Fatalf("accepted invalid value %+v: %s %v", v, body, err)
			}
		}
	})
}
func badTemplate(t *testing.T, d Decision, source, detail string) {
	t.Helper()
	template, err := d.Declare("fixture", source)
	if err == nil || template.Digest() != ([32]byte{}) {
		t.Fatal("unsafe declaration accepted")
	}
	if len(err.Error()) > 256 || !strings.Contains(err.Error(), detail) || !strings.Contains(err.Error(), "fixture") {
		t.Fatalf("refusal %q should name %q", err, detail)
	}
}
