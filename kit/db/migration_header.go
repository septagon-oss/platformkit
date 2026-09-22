package db

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The three kinds a migration file may declare itself to be. Nothing is inferred from
// the SQL: a file that says nothing is an expand, which is what every file in the
// repository was before the header existed and most of them are after it.
const (
	phaseExpand   = "expand"
	phaseContract = "contract"
	phaseData     = "data"
)

// maxBatch bounds `batch=` from above. The bound is not the server's patience: one
// batch is one transaction, and a window wide enough to matter to a ten million row
// table is wide enough to hold locks the running application is waiting on.
const maxBatch = 100000

// minReason is how much of a sentence an `allow=` has to carry. The value is never
// parsed, so this checks nothing about what it says; it checks that the author wrote
// the exception down rather than the shortest thing that satisfies the key, which is
// the whole use of the key.
const minReason = 3

// installBackfillBatches bounds the drain Migrate performs itself: the drain of an
// owner with no history at all (no reader, and no rows but the ones this installation
// just wrote), the resume of one a previous run left unfinished, and the owner's last
// pending data file, which has nothing behind it waiting on the work. Past it the
// process is open too long for a deploy step, and the worker — which drains unbounded
// because nothing waits on its boot — finishes the job, answering with
// ErrBackfillBudget.
const installBackfillBatches = 50

// headerLine is one line of a migration file's header. The marker is the whole
// line, so that a comment which only looks like one is not read as one.
var headerLine = regexp.MustCompile(`^-- pkit:(.*)$`)

// migrationTable is a table name a header may name: one bare identifier, resolved in
// the migration's own search_path like every other name in the file. A qualified value
// or one carrying punctuation is a grammar mistake and, here, the shape of an injection.
var migrationTable = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// migrationHeader is what the runner reads off a file's own declaration. body is
// the file below the header as its author wrote it — what the drain sends to the
// server — and plain is that same body with the comments gone and the case folded,
// which is the one text both the rule table and the drain ask their questions of.
// shape is plain with the contents of every string literal put away as well, which
// is the text the one structural question — does this body read the batch window —
// is asked of, because a body that names the window only inside a value it is
// writing does not read it. A second reading of a second text is how a file gets
// judged for one thing and executed as another, and a value inside the header
// (batch=500) must never be the thing that satisfies a rule about the SQL.
type migrationHeader struct {
	phase       string
	contractOf  int64
	autocommit  bool
	batch       int
	table       string
	body        string
	plain       string
	shape       string
	allowedRule []string
}

// headerKeys are the keys the grammar has. A key outside this list is refused
// rather than ignored: a marker the runner would not read says "this file was
// reviewed" about a file that was not.
var headerKeys = []string{"phase", "expand", "batch", "table", "autocommit", "allow", "reason"}

// parseHeader reads the run of `-- pkit:` lines at the very top of a file and refuses
// every mistake in it. Every refusal names the key and its legal domain, and none is
// exceptable by `allow=` — a marker that excuses a broken marker is one nobody can
// rely on. A key's domain is refused as it is read, and the properties of the whole
// header (a repeated key, a key no file of this shape would read) after it, so what
// reaches the operator is the mistake the file makes rather than the noise after it.
func parseHeader(text string) (migrationHeader, error) {
	h := migrationHeader{phase: phaseExpand}
	lines := strings.Split(text, "\n")
	var keys, unknown, malformed, rules []string
	for i, line := range lines {
		parts := headerLine.FindStringSubmatch(line)
		if parts == nil {
			// The header is the run at the top. A marker anywhere below it is a
			// marker no reader reaches, which is worse than no marker at all.
			if slices.ContainsFunc(lines[i:], headerLine.MatchString) {
				return h, fmt.Errorf("a `-- pkit:` marker below the header is never read: the header is the run of `-- pkit: key=value` lines at the top of the file, before any SQL")
			}
			h.body = strings.Join(lines[i:], "\n")
			break
		}
		rest, ok := strings.CutPrefix(parts[1], " ")
		if !ok || rest == "" {
			malformed = append(malformed, strconv.Quote(line))
			continue
		}
		var lineAllows, lineReason string
		reasons := 0
		for _, pair := range headerPairs(rest) {
			key := pair[0]
			if key == "" {
				malformed = append(malformed, strconv.Quote(pair[1]))
				continue
			}
			if key == "allow" {
				lineAllows = pair[1]
				rules = append(rules, pair[1])
				continue
			}
			if key == "reason" {
				reasons++
				if reasons > 1 {
					return h, fmt.Errorf("two reason= sentences on one line: the line states one sentence about one exception, and a reader that kept only the last would leave the first claiming a review it never got")
				}
				lineReason = pair[1]
				continue
			}
			if err := h.set(key, pair[1]); err != nil {
				return h, err
			}
			if !slices.Contains(headerKeys, key) {
				unknown = append(unknown, key)
				continue
			}
			keys = append(keys, key)
		}
		if lineAllows != "" && !saysSomething(lineReason) {
			return h, fmt.Errorf("allow=%s carries no reason a reviewer could read: the sentence is the whole content of an exception, and a shorter reason than three characters is the empty reason with a letter in front of it (allow=%s reason=<one sentence>)",
				lineAllows, lineAllows)
		}
		if lineAllows == "" && lineReason != "" {
			return h, fmt.Errorf("reason=%s carries no allow=: the sentence belongs to an exception, and this line has nothing to except",
				strconv.Quote(lineReason))
		}
	}
	if len(rules) > 0 {
		h.allowedRule = rules
	}
	if dup := repeatedKey(keys); dup != "" {
		return h, fmt.Errorf("header key %q is repeated; one header states one value per key", dup)
	}
	if err := h.declare(); err != nil {
		return h, err
	}
	var unknownRule []string
	for _, r := range h.allowedRule {
		if !isRule(r) {
			unknownRule = append(unknownRule, r)
		}
	}
	if len(unknownRule) > 0 {
		return h, fmt.Errorf("allow=%s names no rule; the rules are %s, and an exception nobody can name is not one",
			strings.Join(unknownRule, ","), strings.Join(ruleNames(), ", "))
	}
	if len(malformed) > 0 {
		return h, fmt.Errorf("header line %s is not of the form `-- pkit: key=value [key=value …]`", strings.Join(malformed, ", "))
	}
	if len(unknown) > 0 {
		return h, fmt.Errorf("unknown header key %s; the keys are %s — a key the runner does not read claims a review the runner never did",
			strings.Join(unknown, ","), strings.Join(headerKeys, ", "))
	}
	lower := strings.ToLower(h.body)
	h.plain, h.shape = scanSQL(lower, false), scanSQL(lower, true)
	return h, nil
}

// set records one key's value, refusing a value outside its domain.
func (h *migrationHeader) set(key, value string) error {
	switch key {
	case "phase":
		switch value {
		case phaseExpand, phaseContract, phaseData:
			h.phase = value
		default:
			return fmt.Errorf("unknown phase %q; a file is expand (additive, safe beside the release running now), contract (removes what an earlier release expanded) or data (a batched backfill that runs outside one transaction)", strconv.Quote(value))
		}
	case "expand":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 {
			return fmt.Errorf("expand=%s is not a version: the partner names the version of the same owner this file waits for", strconv.Quote(value))
		}
		h.contractOf = n
	case "batch":
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("batch=%s is not a number of rows", strconv.Quote(value))
		}
		// The domain is refused at the key's own parse step, not in declare: the value
		// a file wrote has to be the value the refusal names, and zero is what an
		// absent key leaves behind. A range check that ran after the parse reads
		// batch=0 as a file that wrote no batch at all, and sends the operator to add
		// a line that is already there.
		if n < 1 {
			return fmt.Errorf("batch=%d is outside 1…%d: a window of no rows is a drain that never advances", n, maxBatch)
		}
		if n > maxBatch {
			return fmt.Errorf("batch=%d is outside 1…%d; a window that wide is one transaction holding locks the running application is waiting on", n, maxBatch)
		}
		h.batch = n
	case "table":
		h.table = value
	case "autocommit":
		if value != "true" {
			return fmt.Errorf("autocommit=%s is not a value it takes: the marker takes this file out of the transaction every other file runs in, so it is written as autocommit=true or not at all", strconv.Quote(value))
		}
		h.autocommit = true
	}
	return nil
}

// saysSomething is the domain of a reason=: the value is never parsed and never
// matched — nobody but the reviewer reads it — so all the parser can check is that
// a sentence was written rather than a character.
func saysSomething(reason string) bool { return len([]rune(strings.TrimSpace(reason))) >= minReason }

// declare refuses the combinations no single key's domain covers.
func (h *migrationHeader) declare() error {
	if h.batch != 0 {
		if h.phase != phaseData {
			return fmt.Errorf("batch=%d is only read on a phase=data file: a schema file runs in one transaction, and a window over it would bound nothing", h.batch)
		}
	} else if h.phase == phaseData {
		return fmt.Errorf("a phase=data file carries no batch=: the batch is the bound, and a body with no window is one statement over the whole table")
	}
	if h.table != "" {
		if h.phase != phaseData {
			return fmt.Errorf("table=%s is only read on a phase=data file, which is the file that has to know what it is draining", strconv.Quote(h.table))
		}
		if !migrationTable.MatchString(h.table) {
			return fmt.Errorf("table=%s is not a bare lower-case identifier: the window names the table the way the SQL does, and a value carrying punctuation is neither", strconv.Quote(h.table))
		}
	} else if h.phase == phaseData {
		return fmt.Errorf("a phase=data file carries no table=: the resumable window runs over one table's primary key, and the runner will not guess which")
	}
	if h.autocommit && h.phase == phaseData {
		return fmt.Errorf("autocommit=true is not accepted on a phase=data file: a backfill already runs outside one transaction, in pieces, and takes its atomicity from the batch")
	}
	if h.contractOf != 0 && h.phase != phaseContract {
		return fmt.Errorf("expand=%d is only read on a phase=contract file: it names the expansion this file waits for, and only a file that removes something waits", h.contractOf)
	}
	if h.phase == phaseContract && h.contractOf == 0 {
		return fmt.Errorf("a phase=contract file carries no expand=<version>: the contract half runs a release after the expansion it removes, and the runner cannot tell which expansion that is")
	}
	return nil
}

// keyValue is one header pair, or one pair-shaped thing that was not one.
type keyValue [2]string

// headerPairs splits one header line's content into pairs. Every pair the line
// carries is read, and `reason=` is the only value that may contain spaces: its
// sentence ends at the end of the line or at the next pair the grammar has,
// whichever comes first.
//
// The sentence gives up its tail rather than swallow a declaration. The keys are a
// closed list, so `phase=contract` inside a reason is readable two ways and the
// reading that eats it is the one that hurts: the file stops being the contract half
// its own face says it is, the exception written beside the swallowed key becomes a
// used exception because eating the phase is what makes the rule fire, and the
// marker then reads as the review that phase asked for. Read as a pair instead, the
// declaration stands and the rule table refuses the file for what it now says about
// itself. A sentence that quotes a pair verbatim is therefore read as a pair; write
// the pairs first and the sentence last.
func headerPairs(rest string) []keyValue {
	var pairs []keyValue
	for rest != "" {
		token, remainder, found := strings.Cut(rest, " ")
		if !found {
			remainder = ""
		}
		if key, value, ok := strings.Cut(token, "="); ok && key == "reason" {
			sentence, tail := sentenceEnd(remainder)
			pairs = append(pairs, keyValue{"reason", strings.TrimSpace(value + " " + sentence)})
			rest = strings.TrimLeft(tail, " ")
			continue
		}
		if token != "" {
			if key, value, ok := strings.Cut(token, "="); ok {
				pairs = append(pairs, keyValue{key, value})
			} else {
				malformed := keyValue{}
				malformed[1] = token
				pairs = append(pairs, malformed)
			}
		}
		rest = strings.TrimLeft(remainder, " ")
	}
	return pairs
}

// sentenceEnd is where a reason= stops being prose: the first word on the rest of
// the line that opens a pair the grammar has, which is a declaration and not a
// sentence's vocabulary. Everything before it is the sentence, and everything from
// it on is parsed like any other part of the line — so a pair written after a
// sentence is read, and prose the sentence did not finish is refused as the
// pair-shaped thing it is.
func sentenceEnd(rest string) (sentence, tail string) {
	for start := 0; start < len(rest); {
		width := strings.IndexByte(rest[start:], ' ')
		if width < 0 {
			width = len(rest) - start
		}
		word := rest[start : start+width]
		if key, _, ok := strings.Cut(word, "="); ok && slices.Contains(headerKeys, key) {
			return rest[:start], rest[start:]
		}
		start += width + 1
	}
	return rest, ""
}

func repeatedKey(keys []string) string {
	for i, k := range keys {
		if slices.Contains(keys[:i], k) {
			return k
		}
	}
	return ""
}
