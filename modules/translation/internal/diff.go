// Package internal is the translation service: the rows, the staleness rule,
// and the paragraph diff that says which part of a translation the source moved
// under.
//
// Two things this package does not do, both of them load-bearing.
//
// It never reads another module's table. Which records exist, and what their
// source text says now, arrive through rest.TranslationSource — a port this
// module could not fill if it wanted to, filled at composition with one literal
// line per mounting module. "Missing" is the one question that cannot be
// answered from the translations table alone, because it is a question about
// the *other* table, and the honest answer to "how do I read that?" is "you are
// handed it".
//
// It knows nothing about any field's meaning. The validation a translation gets
// is the field's own, run by kit/rest on the way in; nothing here knows what a
// page, a product description or a legal clause is, and nothing branches on
// which modules are mounted.
package internal

// maxParagraphs bounds the diff. The DP below is O(n·m) in paragraphs, and a
// quarter-megabyte body — the ceiling the one richtext field in this repository
// declares — could hold enough of them to make one page of an overview cost
// seconds. Past the bound the answer is "the whole field is outdated, no
// paragraph-level claim", which is a stated answer rather than a hang, and the
// UI that receives it says so instead of inventing a highlight.
const maxParagraphs = 2000

// Kind is what happened to one paragraph between the source a translation was
// made from and the source as it now stands.
type Kind int

const (
	// Unchanged: the same paragraph, in the same place. A translator never
	// needs to look at it.
	Unchanged Kind = iota
	// Changed: this paragraph of the source replaced that paragraph of the
	// translation's own source. Both are named, because journey 2 highlights the
	// changed paragraph on both sides of the split view.
	Changed
	// Added: the source gained a paragraph this translation never saw.
	Added
	// Removed: the source lost one the translator worked on.
	Removed
)

func (k Kind) String() string {
	switch k {
	case Unchanged:
		return "unchanged"
	case Changed:
		return "changed"
	case Added:
		return "added"
	case Removed:
		return "removed"
	}
	return "unknown"
}

// Change is one paragraph's verdict. SourceIndex is the paragraph's position in
// the source as it now stands, TargetIndex in the source the translation was
// made from; -1 for the side a change has no paragraph on, which is every Add
// and every Remove.
type Change struct {
	SourceIndex, TargetIndex int
	Kind                     Kind
}

// Diff is the whole answer for one field: how the two sources line up, and
// whether the question was too big to answer at paragraph level at all.
type Diff struct {
	Changes []Change
	// All is the bound's answer: the paragraph claim was refused. Everything is
	// outdated, nothing is highlighted, and the banner says so.
	All bool
}

// DiffSource compares the source a translation was made from with the source as
// it now stands, paragraph by paragraph, and says for each paragraph of each
// side whether a translator must look at it.
//
// The algorithm is the one the specification names, and each step is there
// because the alternative fails in a specific way:
//
//  1. Both sides are split by the format's own rules, so the diff joins a block
//     the renderer joins and splits one it splits.
//  2. Each paragraph is keyed by its digest — identity, not similarity.
//  3. Longest common subsequence over the two key sequences gives the anchors.
//  4. Between two consecutive anchors, whatever is left on both sides is a
//     replacement: 1↔1 is one changed paragraph on both sides, and n↔m is the
//     whole block rewritten, with every paragraph on both sides marked. The
//     honest view says "this block was rewritten" rather than inventing a
//     mapping between the second of three old paragraphs and the first of two
//     new ones.
//  5. A paragraph with nothing opposite it is added or removed.
//
// A field is never reported as having changed paragraphs when its digest has
// not changed, because both sides come from the same function over the same
// bytes; that is C21's property, and the reason the diff can be shown at all.
func DiffSource(atTranslation, current string, rich bool) (Diff, error) {
	left, err := paragraphsOf(atTranslation, rich)
	if err != nil {
		return Diff{}, err
	}
	right, err := paragraphsOf(current, rich)
	if err != nil {
		return Diff{}, err
	}
	if len(left) > maxParagraphs || len(right) > maxParagraphs {
		return Diff{All: true}, nil
	}
	d := align(left, right, paragraphKey)
	if !d.All && len(d.Changes) == 0 {
		// Nothing aligned differently. Either the two sources really are the
		// same text, or they differ in something the paragraph split cannot
		// see — a heading marker, a URL. The digest is the authority on that,
		// and the digest is what marked the row outdated in the first place.
		if h1, e1 := Hash(atTranslation, rich); e1 == nil {
			if h2, e2 := Hash(current, rich); e2 == nil && h1 != h2 {
				d.All = true
			}
		}
	}
	return d, nil
}

// align is step 3 and 4: the anchors, then the replacement runs between them.
func align(left, right []string, key func(string) string) Diff {
	lk := make([]string, len(left))
	for i, p := range left {
		lk[i] = key(p)
	}
	rk := make([]string, len(right))
	for j, p := range right {
		rk[j] = key(p)
	}

	// LCS table, longest common subsequence of paragraph keys. O(n·m) cells of
	// int32, which is the bound maxParagraphs exists for.
	n, m := len(lk), len(rk)
	table := make([][]int32, n+1)
	for i := range table {
		table[i] = make([]int32, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if lk[i] == rk[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}

	// Walk the table once, collecting matched pairs — the anchors.
	var anchors []Change
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case lk[i] == rk[j]:
			// The pair is one paragraph the two sources still agree on. Spelled
			// in the Diff's own terms: i walks the source the translation was
			// made from, which is the Target, and j walks the source as it now
			// stands, which is the Source.
			anchors = append(anchors, Change{SourceIndex: j, TargetIndex: i, Kind: Unchanged})
			i, j = i+1, j+1
		case table[i+1][j] >= table[i][j+1]:
			i++
		default:
			j++
		}
	}

	var out Diff
	// emit covers the run of paragraphs between the previous anchor and the
	// next one, on both sides at once — the replacement.
	// lastI and lastJ are one past the previous anchor, so the run of
	// paragraphs before the next anchor is [lastI, nextI): the loop bounds and
	// the sizes below have to agree with each other, and off-by-one here is a
	// diff that silently reports a rewritten paragraph as nothing at all.
	// lastLeft and lastRight are one past the previous anchor, so the run of
	// paragraphs before the next anchor is [lastLeft, nextLeft) on the source
	// the translation was made from and [lastRight, nextRight) on the source as
	// it now stands. The loop bounds and the sizes below have to agree with each
	// other, and off-by-one here is a diff that reports a rewritten paragraph as
	// nothing at all — which is the failure a reviewer can never see.
	emit := func(lastLeft, lastRight, nextLeft, nextRight int) {
		gapLeft, gapRight := nextLeft-lastLeft, nextRight-lastRight
		if gapLeft <= 0 && gapRight <= 0 {
			return
		}
		if gapLeft == 1 && gapRight == 1 {
			out.Changes = append(out.Changes, Change{SourceIndex: lastRight, TargetIndex: lastLeft, Kind: Changed})
			return
		}
		// A replacement: every paragraph of both runs is a paragraph somebody
		// has to look at, and the view says "this block was rewritten".
		for a := lastLeft; a < nextLeft; a++ {
			for b := lastRight; b < nextRight; b++ {
				out.Changes = append(out.Changes, Change{SourceIndex: b, TargetIndex: a, Kind: Changed})
			}
		}
		// And a run with nothing opposite it: the source lost a paragraph the
		// translator worked on, or gained one nobody has translated. The side
		// with no paragraph carries -1, which is what stops a renderer from
		// highlighting a paragraph that is not there.
		for a := lastLeft; a < nextLeft; a++ {
			if gapRight == 0 {
				out.Changes = append(out.Changes, Change{SourceIndex: -1, TargetIndex: a, Kind: Removed})
			}
		}
		for b := lastRight; b < nextRight; b++ {
			if gapLeft == 0 {
				out.Changes = append(out.Changes, Change{SourceIndex: b, TargetIndex: -1, Kind: Added})
			}
		}
	}

	prevLeft, prevRight := 0, 0
	for _, a := range anchors {
		emit(prevLeft, prevRight, a.TargetIndex, a.SourceIndex)
		out.Changes = append(out.Changes, a)
		prevLeft, prevRight = a.TargetIndex+1, a.SourceIndex+1
	}
	emit(prevLeft, prevRight, n, m)
	return out
}
