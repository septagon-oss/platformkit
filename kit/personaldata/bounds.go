package personaldata

// This file answers one question about the columns a scrub is about to rewrite:
// how much room each holds. The scrub needs it because its own replacement can
// make a value longer than the identifier it removed — Subject.Token is seventeen
// characters and a person's name is often shorter — so a row already filling its
// column has nowhere for the extra characters, the statement is refused, and the
// erasure dies leaving the person named in every row it did not reach.
//
// The answer comes from the server rather than a claim beside the scrub, for the
// reason a claimed width goes stale: a migration that widens a column moves the
// ceiling and leaves the claim behind, and a stale ceiling truncates text that no
// longer needs it. The server keeps one column's room in two places — its own
// type, and a CHECK constraint where the schema left the type unbounded — so it is
// asked twice. A bound it states no other way this file cannot see, and such an
// erasure fails loudly rather than silently.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/septagon-oss/platformkit/kit/db"
)

// extent is the room one column holds, in the two measures a schema can bound a column
// by. A zero means the column carries no bound of that measure, which is the same answer
// a text column with no CHECK gives: any number of characters, any number of bytes.
type extent struct {
	// chars is the largest character count the column accepts. length() measures it.
	chars int

	// octets is the largest byte count the column accepts. octet_length() measures it,
	// and it is a different number from chars whenever the text is not plain ASCII, so
	// it gets its own measure rather than being folded into one.
	octets int
}

// room reports whether the server named any bound at all, which is what decides whether
// the rewrite is guarded: an unbounded column has nothing to fit inside.
func (e extent) room() bool { return e.chars > 0 || e.octets > 0 }

// bounds asks the server how much room each named column of table holds. A column the
// server bounds in both measures answers with both, and one it bounds in neither is
// absent from the answer, which is how the caller tells "unbounded" from "one character".
//
// The table is resolved the way the UPDATE resolves it — through the session's
// search_path, with to_regclass — because counting the bare name across every schema
// would answer with another table's columns.
func bounds(ctx context.Context, tx db.Tx[db.Tenant], table string, columns []string) (map[string]extent, error) {
	if len(columns) == 0 {
		return nil, nil
	}
	wanted := make(map[string]bool, len(columns))
	for _, c := range columns {
		wanted[c] = true
	}
	rooms := make(map[string]extent, len(columns))
	if err := typeBounds(ctx, tx, table, wanted, rooms); err != nil {
		return nil, err
	}
	if err := checkBounds(ctx, tx, table, wanted, rooms); err != nil {
		return nil, err
	}
	return rooms, nil
}

// typeBounds reads the width each column's own type carries. atttypmod holds it as the
// declared length plus the four-byte header varlena carries it in, and a value of -1 (or
// 4 or less, for a server that stores nothing) is a varchar declared with no length at
// all, which is the same answer text gives.
func typeBounds(ctx context.Context, tx db.Tx[db.Tenant], table string, wanted map[string]bool, rooms map[string]extent) error {
	var found []struct {
		Name  string
		Chars *int
	}
	err := tx.DB().WithContext(ctx).Raw(`SELECT a.attname::text AS name,
       CASE WHEN t.typname IN ('varchar', 'bpchar') AND a.atttypmod > 4
            THEN a.atttypmod - 4 END AS chars
FROM pg_attribute a
JOIN pg_type t ON t.oid = a.atttypid
WHERE a.attrelid = to_regclass(?) AND a.attnum > 0 AND NOT a.attisdropped`, table).Scan(&found).Error
	if err != nil {
		return fmt.Errorf("personaldata: the width of %s's columns: %w", table, err)
	}
	for _, f := range found {
		if f.Chars == nil || !wanted[f.Name] {
			continue
		}
		room := rooms[f.Name]
		room.chars = narrower(room.chars, *f.Chars)
		rooms[f.Name] = room
	}
	return nil
}

// checkBounds reads the bound each column carries as a CHECK constraint instead of in its
// type, out of the text the server prints for the constraint.
//
// Only a validated constraint is asked, because a NOT VALID one is a rule the server does
// not enforce on rows already written, and a bound an existing row may already step past
// would be answered with a truncation of somebody's text rather than with a refusal.
// A deferrable constraint is asked too: it is enforced by commit time, which is when the
// scrub's own transaction ends.
func checkBounds(ctx context.Context, tx db.Tx[db.Tenant], table string, wanted map[string]bool, rooms map[string]extent) error {
	var found []struct {
		Clause string
	}
	err := tx.DB().WithContext(ctx).Raw(`SELECT pg_get_constraintdef(c.oid) AS clause
FROM pg_constraint c
WHERE c.contype = 'c' AND c.convalidated AND c.conrelid = to_regclass(?)`, table).Scan(&found).Error
	if err != nil {
		return fmt.Errorf("personaldata: the CHECK bounds of %s's columns: %w", table, err)
	}
	for _, f := range found {
		for column, room := range clauseBounds(f.Clause, wanted) {
			rooms[column] = tighter(rooms[column], room)
		}
	}
	return nil
}

// clauseBounds reads the bounds one CHECK constraint guarantees for the columns the scrub
// is about to rewrite, out of the text the server prints for it.
//
// What the constraint says about a column it does not measure is nobody's bound, so a
// column nothing in the clause names is absent from the answer rather than unbounded.
func clauseBounds(constraint string, wanted map[string]bool) map[string]extent {
	found := map[string]extent{}
	for _, clause := range conjuncts(constraint) {
		column, measure, limit, ok := sizeClause(clause)
		if !ok || !wanted[column] {
			continue
		}
		room := found[column]
		if measure == measureOctets {
			room.octets = narrower(room.octets, limit)
		} else {
			room.chars = narrower(room.chars, limit)
		}
		found[column] = room
	}
	return found
}

// tighter merges two answers about one column, keeping the smaller bound of each measure,
// where a zero says no answer of that measure exists and so loses to any number.
func tighter(have, more extent) extent {
	return extent{chars: narrower(have.chars, more.chars), octets: narrower(have.octets, more.octets)}
}

// narrower keeps the smaller of two bounds, where zero means "no bound of this measure"
// and so loses to any number.
func narrower(have, declared int) int {
	if have == 0 || (declared > 0 && declared < have) {
		return declared
	}
	return have
}

// lengthMeasure names which of the server's two length functions a clause measures.
type lengthMeasure int

const (
	measureChars lengthMeasure = iota
	measureOctets
)

// sizeClause reads one guaranteed clause of the shape "this column is no longer than N",
// written as the server's deparser writes it — `char_length(body) <= 262144`,
// `length((title)::text) <= 200`, `octet_length(body) <= 1048576` — and answers false for
// everything else, which is most of what a CHECK holds and the honest answer for it.
//
// The three function names are the whole vocabulary Postgres prints for a length: length
// and char_length count characters and octet_length counts bytes. A clause that measures
// anything else — a pattern match, an equality, an IN list — names no bound this scrub
// can keep inside, so it is skipped rather than guessed at.
func sizeClause(clause string) (column string, measure lengthMeasure, limit int, ok bool) {
	left, right, split := strings.Cut(clause, " <= ")
	if !split {
		return "", measureChars, 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(right))
	if err != nil || n <= 0 {
		return "", measureChars, 0, false
	}
	fn, argument, cut := strings.Cut(strings.TrimSpace(left), "(")
	if !cut || !strings.HasSuffix(argument, ")") {
		return "", measureChars, 0, false
	}
	switch fn {
	case "length", "char_length":
		measure = measureChars
	case "octet_length":
		measure = measureOctets
	default:
		return "", measureChars, 0, false
	}
	name := strings.TrimSpace(argument[:len(argument)-1])
	if before, _, cast := strings.Cut(name, "::"); cast {
		name = strings.TrimSpace(before)
	}
	name = unwrap(strings.TrimSpace(name))
	if !namePattern.MatchString(name) {
		return "", measureChars, 0, false
	}
	return name, measure, n, true
}

// conjuncts splits the text the server prints for one CHECK constraint into the clauses
// that must each hold for every row: the leaves of its top-level AND tree, with the
// parentheses its deparser puts around every expression taken back off.
//
// Stopping at an OR is what makes the answer safe rather than merely plausible. A bound
// behind an OR is not a bound — `CHECK (length(note) <= 10 OR kind = 'long')` lets a row
// hold five thousand characters the moment its kind is 'long' — and a scrub that read 10
// there would cut a colleague's note down to ten characters to erase one mention inside
// it. Destroying the writing of somebody who is not the subject is the one failure this
// package is written not to make, so a clause is read for its bound only when nothing
// made it conditional.
func conjuncts(clause string) []string {
	var out []string
	var walk func(string)
	walk = func(s string) {
		s = unwrap(strings.TrimSpace(s))
		parts := splitTop(s, " AND ")
		if len(parts) < 2 {
			out = append(out, s)
			return
		}
		for _, part := range parts {
			walk(part)
		}
	}
	// The word the server puts in front of every constraint definition is not part of the
	// expression, and leaving it in would hide the outermost parenthesis from the unwrap.
	if rest, keyword := strings.CutPrefix(strings.TrimSpace(clause), "CHECK "); keyword {
		clause = rest
	}
	walk(clause)
	return out
}

// splitTop cuts a clause at every sep that sits outside every parenthesis, so the cut is
// made at the level the server wrote it at and never inside `(a OR b) AND c`.
func splitTop(s, sep string) []string {
	depths := depths(s)
	var out []string
	start := 0
	for i := 0; i+len(sep) <= len(s); i++ {
		if depths[i] != 0 || s[i:i+len(sep)] != sep {
			continue
		}
		out = append(out, s[start:i])
		i += len(sep) - 1
		start = i + 1
	}
	return append(out, s[start:])
}

// unwrap drops a pair of parentheses when one wraps the whole clause, as many times as
// the server wrote them: `CHECK (((a) AND (b)))` reaches here as `((a) AND (b))`, and the
// outer pair belongs to the CHECK, not to the clause.
func unwrap(s string) string {
	for wrapsAll(s) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// wrapsAll reports whether the first parenthesis of s closes at its last character,
// which is the test that tells `(a AND b)` from `(a) AND (b)` — both of which start with
// one parenthesis and end with one.
func wrapsAll(s string) bool {
	if len(s) < 2 || s[0] != '(' || s[len(s)-1] != ')' {
		return false
	}
	for i, depth := range depths(s) {
		if s[i] != ')' || depth != 1 {
			continue
		}
		// The first parenthesis that closes the one this clause starts with: the wrap is a
		// wrap only when nothing else was left to close after it.
		return i == len(s)-1
	}
	return false
}

// depths answers, for every byte of a clause, how many parentheses enclose it, with
// quoted literals folded out of the count. The fold is not decoration: a CHECK over
// somebody's vocabulary — `CHECK (kind IN ('a)b', 'c'))` — carries a parenthesis inside a
// literal and an AND inside the next one, and a scan that counted it would split clauses
// that were never alternatives and read a bound nobody wrote.
func depths(clause string) []int {
	out := make([]int, len(clause))
	depth := 0
	for i := 0; i < len(clause); i++ {
		out[i] = depth
		switch clause[i] {
		case '(':
			depth++
		case ')':
			depth--
		case '\'':
			for i++; i < len(clause); i++ {
				out[i] = depth
				if clause[i] != '\'' {
					continue
				}
				if i+1 < len(clause) && clause[i+1] == '\'' {
					out[i+1] = depth
					i++
					continue
				}
				break
			}
		}
	}
	return out
}
