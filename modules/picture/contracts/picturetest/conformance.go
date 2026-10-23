package picturetest

import (
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
	t.Run("literal output and identities", func(t *testing.T) { literalOutput(t, factory) })
	t.Run("UTF-8 declarations", func(t *testing.T) { utf8Declarations(t, factory) })
	t.Run("closed element allowlist", func(t *testing.T) { closedElements(t, factory) })
	t.Run("closed attribute allowlist", func(t *testing.T) { closedAttributes(t, factory) })
	t.Run("unsafe paint", func(t *testing.T) { unsafePaint(t, factory) })
	invalidTemplates(t, factory)
	t.Run("invalid numeric geometry", func(t *testing.T) { invalidGeometry(t, factory) })
	t.Run("invalid values", func(t *testing.T) { invalidValues(t, factory) })
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
