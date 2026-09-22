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

// installBackfillBatches bounds the drain Migrate performs itself: the drain of an
// owner with no history at all (no reader, and no rows but the ones this installation
// just wrote) and the resume of one a previous run left unfinished. Past it the process
// is open too long for a deploy step, and the worker — which drains unbounded because
// nothing waits on its boot — finishes the job, answering with ErrBackfillBudget.
const installBackfillBatches = 50

// headerLine is one line of a migration file's header. The marker is the whole
// line, so that a comment which only looks like one is not read as one.
var headerLine = regexp.MustCompile(`^-- pkit:(.*)$`)

// migrationTable is a table name a header may name: one bare identifier, resolved in
// the migration's own search_path like every other name in the file. A qualified value
// or one carrying punctuation is a grammar mistake and, here, the shape of an injection.
var migrationTable = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// migrationHeader is what the runner reads off a file's own declaration. body is
// the file below the header: what a data migration wraps and what the rule table
// reads, so that a value inside the header (batch=500) can never be the thing
// that satisfies a rule about the SQL.
type migrationHeader struct {
	phase       string
	contractOf  int64
	autocommit  bool
	batch       int
	table       string
	body        string
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
		if lineAllows != "" && strings.TrimSpace(lineReason) == "" {
			return h, fmt.Errorf("allow=%s carries no reason=: the sentence is the whole content of an exception, and without it the marker is a bypass with a name on it (allow=%s reason=<one sentence>)",
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

// declare refuses the combinations no single key's domain covers.
func (h *migrationHeader) declare() error {
	if h.batch != 0 {
		if h.phase != phaseData {
			return fmt.Errorf("batch=%d is only read on a phase=data file: a schema file runs in one transaction, and a window over it would bound nothing", h.batch)
		}
		if h.batch < 1 || h.batch > maxBatch {
			return fmt.Errorf("batch=%d is outside 1…%d; a window that wide is one transaction holding locks the running application is waiting on", h.batch, maxBatch)
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

// headerPairs splits one header line's content into pairs. A reason= value runs to
// the end of its line, because it is a sentence; every other value is one token.
func headerPairs(rest string) []keyValue {
	var pairs []keyValue
	for rest != "" {
		if value, ok := strings.CutPrefix(rest, "reason="); ok {
			return append(pairs, keyValue{"reason", value})
		}
		token, remainder, found := strings.Cut(rest, " ")
		if !found {
			remainder = ""
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

func repeatedKey(keys []string) string {
	for i, k := range keys {
		if slices.Contains(keys[:i], k) {
			return k
		}
	}
	return ""
}
