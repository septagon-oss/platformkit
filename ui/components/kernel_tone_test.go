package components

// kernel_tone_test.go binds entity.Tones to clBadgeTone. The kernel names the
// tones a status may be said in, and this package is what draws one; kit may not
// import ui, so the tie is a test here.
//
// The direction matters: every kernel tone must have a class list, so a seventh
// name added there without one here is red. The reverse is not asserted —
// `brand` is drawn here for a component affordance and is deliberately not a
// tone a status may wear, which is why badge.go substituting `neutral` for an
// unknown tone stays right as a render-time default and would be wrong as a
// declaration default.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func TestEveryKernelToneHasABadgeTone(t *testing.T) {
	t.Parallel()
	for _, tone := range entity.Tones {
		if _, ok := clBadgeTone[tone]; !ok {
			t.Errorf("entity.Tones admits %q and clBadgeTone draws no class list for it", tone)
		}
	}
}
