package wire

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// RequirednessAllowance is one reviewed move of one published response member from
// required to optional. Record is the whole requirement record B3 names — the
// operation, the node and the member, spelled as the refusal spells them — so one
// allowance covers one member of one node of one operation and reaches nothing else.
// ReviewedOn is a YYYY-MM-DD review date, not an expiry or a wall-clock check, and
// Reason says what the move was for.
type RequirednessAllowance struct {
	Record     string
	ReviewedOn string
	Reason     string
}

// CompareWithRequiredness is CompareWithAllowances with the caller's reviewed
// requiredness list. Each allowance removes exactly one B3 record, the requirement it
// names, and only after reviewRequiredness held the list at the gate; every rule and
// every other record is compared as it always was. Read-only calls are safe to run
// concurrently.
func CompareWithRequiredness(golden, current []byte, allowances []AuthorizationAllowance, required []RequirednessAllowance) []Break {
	return compareWire(golden, current, allowances, required)
}

// reviewRequiredness refuses a list that a reviewer did not sign record by record, or
// that could carry more than the one move it names. The member and its node have to be
// recorded on both sides with the same shape, so no allowance can excuse a retype, a
// reference moved out from under a reader, or the member's disappearance: what is left
// for one to excuse is the requirement alone. Checked that way, an allowance stays
// valid after the relaxation it records has landed, when both documents hold the member
// optional — it then names a state rather than a move, and only the day the member
// itself goes away does it refuse, which is when the record is stale for good.
func reviewRequiredness(required []RequirednessAllowance, oldShapes, newShapes map[string]string) []Break {
	var problems []Break
	seen := map[string]bool{}
	for i, a := range required {
		if problem := requirednessProblem(a, seen, oldShapes, newShapes); problem != "" {
			text := fmt.Sprintf("B3 (breaking): requiredness allowance #%d (%s) %s", i, a.Record, problem)
			problems = append(problems, Break{"B3", "$requiredness", "#" + strconv.Itoa(i), text})
		}
		seen[a.Record] = true
	}
	return problems
}

// requirednessProblem states why this allowance cannot stand, or "" when it can. A
// record reads `<operation> response <status> <media> <address> requires <member>`:
// the requiredness of what a caller must send is B4's decision, and no allowance
// written here reaches it.
func requirednessProblem(a RequirednessAllowance, seen map[string]bool, oldShapes, newShapes map[string]string) string {
	if a.Record == "" || seen[a.Record] || strings.TrimSpace(a.Reason) == "" || !wireDate(a.ReviewedOn) {
		return "names no one reviewed move: it needs a record named once, a YYYY-MM-DD date and a reason"
	}
	cut := strings.LastIndex(a.Record, " requires ")
	if cut < 1 || cut+len(" requires ") == len(a.Record) {
		return "is not the requiredness of one member"
	}
	node, member := a.Record[:cut], a.Record[cut+len(" requires "):]
	if id, address, _ := strings.Cut(node, " "); id == "" || !strings.HasPrefix(address, "response ") {
		return "names no response; what a caller must send belongs to B4"
	}
	for _, record := range []string{node, node + " ." + member} {
		was, oldOK := oldShapes[record]
		now, newOK := newShapes[record]
		if !oldOK || !newOK || was != now {
			return "does not hold " + record + " recorded the same on both sides"
		}
	}
	return ""
}

// wireDate reports whether text is a calendar date in the one spelling a review is
// dated with: a four-digit year, a two-digit month and a two-digit day, each reading
// back as itself.
func wireDate(text string) bool {
	day, err := time.Parse("2006-01-02", text)
	return err == nil && day.Format("2006-01-02") == text
}

// requirednessRelaxation reports whether a dropped requirement is the one an
// allowance reviewed. The rules refuse it until then; this only asks whose review it was.
func requirednessRelaxation(required []RequirednessAllowance, key string) bool {
	return slices.ContainsFunc(required, func(a RequirednessAllowance) bool { return a.Record == key })
}
