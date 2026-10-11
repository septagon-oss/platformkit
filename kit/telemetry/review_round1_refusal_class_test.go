package telemetry_test

// Review round 1 (T-0110). RefusalClass's comment makes a promise about the set:
//
//   It is closed, so a new refusal reason lands in one of these twelve and never in
//   a class somebody invented at a call site.
//
// A reason does not have to be invented at a call site to be missing from the table.
// kit/httpx/authorize.go answers 402 Payment Required with CodePlanExcludes — a
// refusal this kernel writes on purpose, on the plan gate — and 402 is not in the
// switch, so the number files it under the fallback "unknown", which is exactly the
// reading the comment rules out: an operator filtering by class cannot tell "the plan
// does not include this" from "nobody knew what this was".

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/telemetry"
)

func TestEveryRefusalThisKernelWritesHasAClassOfItsOwn(t *testing.T) {
	// The statuses the two refusal writers actually answer, read off the call sites:
	//   7 StatusInternalServerError, 6 StatusNotFound, 4 StatusServiceUnavailable,
	//   3 StatusForbidden, 1 StatusTooManyRequests, 1 StatusPaymentRequired,
	//   1 StatusMethodNotAllowed
	for _, status := range []int{
		500, 503, 404, 403, 429, 402, 405,
	} {
		class := telemetry.RefusalClass(status)
		if class == "unknown" {
			t.Errorf("RefusalClass(%d) = %q; a refusal kit/httpx writes on purpose has no class in the "+
				"closed set, so pkit.http.refusals counts it beside every other unmapped answer and an "+
				"operator cannot tell them apart", status, class)
		}
	}
}
