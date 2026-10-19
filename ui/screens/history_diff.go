package screens

// history_diff.go compares the two values one field held, word by word, so a
// version says what moved instead of repeating itself.
//
// A version stores the whole record, which is what lets a restore replay it, and it
// is the wrong thing to show: two versions of a body differ by a sentence and a
// person comparing them should not have to read both. So the panel shows the words
// the write kept, the words it added and the words it dropped. The comparison is
// over the stored text rather than the rendered document because what a version keeps
// is the source — rendering it would need the request's transaction, the file store
// and the rich-text renderer standing between a person and what they typed, and a
// picture that moved would arrive as an id either way.
//
// The middle of a value is bounded: two versions sharing no word are the whole of a
// document rewritten, and no reader wants that aligned word for word. It is shown as
// one removal and one insertion, which is the truth at that size. The bound costs the
// comparison its exactness only for a value nobody can read in one screenful anyway;
// the versions route still answers both documents in full.

import (
	"strings"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// comparedWords is how many words either side of an alignment may hold before the
// comparison gives up on matching them. Two hundred words each way is the table the
// alignment keeps in memory — 200×200 ranks — and past that the two values are a
// rewrite, not an edit.
const comparedWords = 200

// wordEdits is the comparison of two values of one field: the words both hold, the
// words after added, the words after no longer holds, in the order they read. Each
// stretch carries its own spacing, so the whole sequence joined reads as the value.
func wordEdits(before, after string) []resource.VersionEdit {
	kept, added, dropped := resource.EditKept, resource.EditInserted, resource.EditRemoved
	var edits []resource.VersionEdit
	// append merges a run rather than starting a new element per word: an inserted
	// clause is one insertion, and a paragraph wearing an element per word is markup
	// a screen reader reads out as a stutter.
	appendEdit := func(kind resource.EditKind, words []string) {
		if len(words) == 0 {
			return
		}
		text := strings.Join(words, " ") + " "
		if last := len(edits) - 1; last >= 0 && edits[last].Kind == kind {
			edits[last].Text += text
			return
		}
		edits = append(edits, resource.VersionEdit{Text: text, Kind: kind})
	}

	oldWords, newWords := strings.Fields(before), strings.Fields(after)
	head := 0
	for head < len(oldWords) && head < len(newWords) && oldWords[head] == newWords[head] {
		head++
	}
	tail := 0
	for tail < len(oldWords)-head && tail < len(newWords)-head &&
		oldWords[len(oldWords)-1-tail] == newWords[len(newWords)-1-tail] {
		tail++
	}
	// The shared beginning and end are cut first for both reasons: they are what a
	// reader would call the unchanged part, and they are what is left out of the
	// table below, which is what keeps an edit of one word in a long body cheap.
	appendEdit(kept, oldWords[:head])
	oldMid, newMid := oldWords[head:len(oldWords)-tail], newWords[head:len(newWords)-tail]
	if len(oldMid) > comparedWords || len(newMid) > comparedWords {
		appendEdit(dropped, oldMid)
		appendEdit(added, newMid)
	} else {
		ranks := commonRanks(oldMid, newMid)
		i, j := 0, 0
		for i < len(oldMid) && j < len(newMid) {
			switch {
			case oldMid[i] == newMid[j]:
				appendEdit(kept, oldMid[i:i+1])
				i++
				j++
			case ranks[i+1][j] >= ranks[i][j+1]:
				appendEdit(dropped, oldMid[i:i+1])
				i++
			default:
				appendEdit(added, newMid[j:j+1])
				j++
			}
		}
		appendEdit(dropped, oldMid[i:])
		appendEdit(added, newMid[j:])
	}
	appendEdit(kept, oldWords[len(oldWords)-tail:])
	return edits
}

// commonRanks answers, for every pair of positions, how many words the two middles
// hold in common from there on. The walk over it from the front is what writes the
// comparison above; the table is filled from the back so each cell is one look at
// its two neighbours.
func commonRanks(oldWords, newWords []string) [][]int {
	ranks := make([][]int, len(oldWords)+1)
	for i := range ranks {
		ranks[i] = make([]int, len(newWords)+1)
	}
	for i := len(oldWords) - 1; i >= 0; i-- {
		for j := len(newWords) - 1; j >= 0; j-- {
			if oldWords[i] == newWords[j] {
				ranks[i][j] = ranks[i+1][j+1] + 1
				continue
			}
			ranks[i][j] = max(ranks[i+1][j], ranks[i][j+1])
		}
	}
	return ranks
}
