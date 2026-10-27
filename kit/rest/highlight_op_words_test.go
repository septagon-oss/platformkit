package rest

// The statement grammar narrows a page with more operators than a declared sub-count
// asks with, and the two lists are not the same list. A refusal that named the wider
// one would tell a module author to write an operator the same gate refuses two lines
// later, so the gate names what it admits.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

func TestADeclaredHighlightRefusesTheOperatorsAPageNarrowsWith(t *testing.T) {
	for _, op := range []crud.Op{crud.OpNe, crud.OpIn, crud.OpIsNull, crud.OpNotNull, crud.OpEqFold} {
		bad := faultOf(&entity.EntryHighlight{Label: "Anything but closed",
			Filter: []entity.EntryPredicate{{Field: "flags", Op: string(op), Value: "m"}}}, "")
		if !strings.Contains(bad, "a declared count") {
			t.Errorf("%s is refused by a sentence that names no vocabulary the author can act on: %q", op, bad)
		}
		for _, admitted := range declaredOps {
			if !strings.Contains(bad, admitted) {
				t.Errorf("%s is refused with a sentence that never names %s, which a declared count does ask with: %q", op, admitted, bad)
			}
		}
		// The gate says what a highlight admits, not what the statement grammar can
		// spell: naming crud.Ops here would send the author to write an operator this
		// same answer refuses.
		for _, pageOnly := range []string{string(crud.OpIn), string(crud.OpIsNull), string(crud.OpEqFold)} {
			if strings.Contains(bad, "admits "+pageOnly) || strings.HasSuffix(bad, pageOnly) {
				t.Errorf("%s is refused with a sentence offering %s: %q", op, pageOnly, bad)
			}
		}
	}
	if bad := faultOf(dueToday(), ""); bad != "" {
		t.Errorf("the sound declaration is refused: %s", bad)
	}
}
