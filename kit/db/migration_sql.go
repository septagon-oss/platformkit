package db

import (
	"regexp"
	"strings"
)

// blockCommentEnd is how many bytes a block comment starting at the front of text
// takes up, nesting included (`/* … /* … */ … */` is one comment to PostgreSQL, which
// is what makes `--` too narrow a rule for where commentary ends). An unterminated one
// runs to the end of the file, which is what the server reads there as well: it then
// refuses the file as a syntax error, and the guard has nothing to judge.
func blockCommentEnd(text string) int {
	for depth, i := 0, 0; i < len(text); {
		switch {
		case strings.HasPrefix(text[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(text[i:], "*/"):
			if depth == 1 {
				return i + 2
			}
			depth--
			i += 2
		default:
			i++
		}
	}
	return len(text)
}

// dollarTag returns a leading dollar-quote delimiter, or empty when none opens.
// Tags accept PostgreSQL identifier bytes, including bytes above ASCII, but no
// leading digit; this keeps positional parameters outside literal bodies.
func dollarTag(text string) string {
	if text == "" || text[0] != '$' {
		return ""
	}
	for i := 1; i < len(text); i++ {
		switch c := text[i]; {
		case c == '$':
			return text[:i+1]
		case !isIdentByte(c) || i == 1 && !isIdentStart(c):
			return ""
		}
	}
	return ""
}

// dollarValueEnd is the index one dollar-quoted value opening at i of text ends at —
// past its closing tag, or at the end of the text where no closing tag ever comes. The
// second answer is what the server is left with too, and it refuses the file for it
// (`unterminated dollar-quoted string`) rather than read the rest of the file as data.
func dollarValueEnd(text string, i int) int {
	tag := dollarTag(text[i:])
	if closed := strings.Index(text[i+len(tag):], tag); closed >= 0 {
		return i + 2*len(tag) + closed
	}
	return len(text)
}

// isIdentStart and isIdentByte are PostgreSQL's `ident_start` and `ident_cont`: an
// underscore, an ASCII letter, any byte ≥ 0x80 (a name takes the database's own letters,
// which is the argument sqlName and dollarTag each make for themselves), and — past the
// first byte of the name — a digit.
func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

func isIdentByte(c byte) bool { return isIdentStart(c) || c >= '0' && c <= '9' }

// isEscapeString is the E'…' prefix, which PostgreSQL reads as one token only where a
// name could not continue: the space in `time '3 days'` is what keeps a type name
// ending in e from swallowing the value after it, and the same space decides this —
// tested against the same byte the two name readers test, because the e at the end of an
// accented name is inside a name and not the prefix of a value.
func isEscapeString(text string, i int) bool {
	if i > 0 && isIdentByte(text[i-1]) {
		return false
	}
	return (text[i] == 'e' || text[i] == 'E') && strings.HasPrefix(text[i+1:], "'")
}

// splitStatements breaks the body on semicolons outside quoted text. It is the
// same cut the measured rule floors were taken with, and it is not a parser: a
// body written inside a dollar-quoted function is split where its semicolons are,
// which is the false positive the exception marker exists for. Nothing else about such a
// body is read at all: see splitTopLevel.
//
// What that sentence licenses, and what it never did, is worth stating exactly: it
// licenses a *rule* to read a construct it may be wrong about, because the file can answer
// the rule with `allow=` and a sentence. It does not license a refusal the file cannot
// answer that way — see splitServerStatements for the three questions that have no marker,
// which are three this split used to answer.
func splitStatements(body string) []string { return splitTopLevel(body, ';', true) }

// splitServerStatements cuts only where PostgreSQL ends a statement, preserving
// dollar bodies and other quoted values whole. The executor and rules that allow
// no exception require this boundary rather than the body-reading approximation.
func splitServerStatements(body string) []string { return splitTopLevel(body, ';', false) }

// eachDollarBody calls body with the contents of every dollar-quoted value in text, the
// two delimiters gone. It is the one place the runner looks inside a value at what the
// value would run, and the two readers below share it because they share the question:
// where the value begins and ends is `dollarTag` and `dollarValueEnd`'s, the same pair
// `sqlToken` gives both of the file's readings.
//
// A value is read whether or not it is a routine body, because telling a `DO $$ … $$`
// block from a string a backfill is writing is a fact about the statement carrying it, and
// the guard does not parse. What that costs is stated where each reader is: a statement
// inside a value that is not a body has to reach the rule's anchor anyway, which is the one
// thing both readings refuse to do — read the words of a value as a statement — and the one
// unmarked question asked of a body is asked for the presence of a word rather than the
// shape of a statement, so no file is refused for a construct this reader invented.
func eachDollarBody(text string, body func(contents string)) {
	for i := 0; i < len(text); {
		tag := dollarTag(text[i:])
		if tag == "" {
			i++
			continue
		}
		end, open := dollarValueEnd(text, i), i+len(tag)
		closed := end - len(tag)
		if closed < open {
			// A value the file never closed: the server refuses the file for it
			// (`unterminated dollar-quoted string`), and there are no contents to read.
			closed = open
		}
		body(text[open:closed])
		i = end
	}
}

// dollarBodyStatements exposes statements inside dollar bodies to anchored rules.
// It reads conditional branches regardless of whether they execute, but stops at
// inner literals and does not interpret dynamically assembled SQL. Allowance
// markers handle conservative matches; see README.md.
func dollarBodyStatements(text string) []string {
	var out []string
	eachDollarBody(text, func(contents string) {
		for _, statement := range splitStatements(contents) {
			for round := 0; round < 8; round++ {
				// A nested block opens with its own `BEGIN` in front of the verb, so the run of
				// control words is taken until the front is a statement's; the bound is because
				// the alternation always leaves something behind on the pass that matches.
				shorter := plpgsqlOpener.ReplaceAllString(statement, "")
				if shorter == statement {
					break
				}
				statement = shorter
			}
			if statement = strings.TrimSpace(statement); statement != "" {
				out = append(out, statement)
			}
		}
	})
	return out
}

// dollarBodySQL is the contents of every dollar-quoted value with its own comments and
// literals put away — the SQL a stored body would run, in the same text `migration.shape`
// is the file's own SQL in. One word is read of it, so nothing about a statement's shape
// is decided here: inside the value, as outside one, a word that survives the put-away is
// a word the body says in SQL rather than one it stores.
func dollarBodySQL(text string) string {
	var out strings.Builder
	eachDollarBody(text, func(contents string) {
		out.WriteString(scanSQL(contents, true))
		out.WriteByte('\n')
	})
	return out.String()
}

// plpgsqlOpener removes supported block, branch, loop and label prefixes before
// reading a body statement. CASE arms remain outside this lexical approximation;
// THEN inside a branch expression can also over-read its prefix.
var plpgsqlOpener = regexp.MustCompile(`(?s)^\s*(?:begin\b|if\b.*?\bthen\b|elsif\b.*?\bthen\b|else\b|while\b.*?\bloop\b|for\b.*?\bloop\b|foreach\b.*?\bloop\b|loop\b|<<.*?>>)\s*`)

// splitClauses breaks one statement on the commas between its ALTER TABLE actions,
// which is where one column definition ends and the next begins. Parentheses are
// counted because a DEFAULT is allowed to be a call: `DEFAULT coalesce(x, 0)` is one
// value and not two clauses, and for the same reason a dollar-quoted body is not cut here
// at all: the comma this splitter is for separates two SQL actions, and a comma inside one
// value separates nothing.
func splitClauses(statement string) []string { return splitTopLevel(statement, ',', false) }

// splitTopLevel cuts at separators outside quoted tokens and parentheses,
// dropping empty pieces. With bodyCuts, separators inside dollar bodies also cut,
// but their quotes and parentheses remain data. All token boundaries come from
// sqlToken, shared with scanSQL.
func splitTopLevel(text string, sep byte, bodyCuts bool) []string {
	var out []string
	var buf []byte
	depth := 0
	for i := 0; i < len(text); {
		if tag := dollarTag(text[i:]); tag != "" {
			end := dollarValueEnd(text, i)
			for bodyCuts && i < end {
				if text[i] == sep {
					out = append(out, string(buf))
					buf = buf[:0]
				} else {
					buf = append(buf, text[i])
				}
				i++
			}
			buf = append(buf, text[i:end]...)
			i = end
			continue
		}
		if width := sqlToken(text, i); width > 0 {
			buf = append(buf, text[i:i+width]...)
			i += width
			continue
		}
		switch c := text[i]; {
		case c == '(':
			depth++
			buf = append(buf, c)
		case c == ')':
			depth = max(depth-1, 0)
			buf = append(buf, c)
		case c == sep && depth == 0:
			out = append(out, string(buf))
			buf = buf[:0]
			i++
			continue
		default:
			buf = append(buf, c)
		}
		i++
	}
	if last := string(buf); strings.TrimSpace(last) != "" {
		out = append(out, last)
	}
	return out
}

// scanSQL blanks comments and, when blankLiterals is set, quoted value and
// identifier contents. sqlToken supplies shared boundaries for nested comments,
// dollar bodies, ordinary strings and escape strings; splitTopLevel decides
// whether separators inside a dollar body count.
func scanSQL(text string, blankLiterals bool) string {
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); {
		if width := sqlToken(text, i); width > 0 {
			token := text[i : i+width]
			i += width
			// Commentary is put away in both readings — that is what "the guard reads the
			// file with its comments gone" means, and the statement the rules anchor on
			// (`^alter table`, `^create index`) starts where the comment above it ends —
			// while the contents of a value are put away only for the shape, which is the
			// one question asked of text with the words taken out of it.
			if blankLiterals || strings.HasPrefix(token, "--") || strings.HasPrefix(token, "/*") {
				out.WriteString(blankSQLToken(token))
				continue
			}
			out.WriteString(token)
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

// sqlToken is how many bytes the comment, dollar-quoted value, escape string, string
// literal or quoted name opening at index i of text takes up, and 0 where none of them
// opens. It is the one place the runner decides where such a construct ends, and it is
// shared because two readers have to agree about it: scanSQL reads a construct to put it
// away (or to keep it whole), and splitTopLevel reads it to know which of its bytes may
// cut a statement — a separator inside any of them is data. A width rather than an end
// index, so the caller keeps walking in the units the text arrives in, and 0 for the
// ordinary bytes, which no caller wants to copy twice over.
func sqlToken(text string, i int) int {
	switch {
	case strings.HasPrefix(text[i:], "--"):
		// The newline stays outside the token: it separated those two lines for the server
		// as well, so the reader that puts the commentary away keeps the line break.
		if end := strings.IndexByte(text[i:], '\n'); end >= 0 {
			return end
		}
		return len(text) - i
	case strings.HasPrefix(text[i:], "/*"):
		// Consumed as commentary in both readings, and nested, which is what PostgreSQL does
		// with it: the apostrophe in `/* it's a backfill */` is not the opening of a literal,
		// and the newline it spans is not a line boundary.
		return blockCommentEnd(text[i:])
	case dollarTag(text[i:]) != "":
		// One value, written with whatever quotes and semicolons it likes: joined to the
		// other literals, which means put away for the shape and kept whole for the text.
		return dollarValueEnd(text, i) - i
	case isEscapeString(text, i):
		for j := i + 2; j < len(text); j++ {
			if text[j] == '\\' {
				j++ // the escape takes the next character, closing quote included
			} else if text[j] == '\'' {
				if j+1 < len(text) && text[j+1] == '\'' {
					j++ // '' is one quote written twice, as in any other literal
					continue
				}
				return j + 1 - i
			}
		}
		return len(text) - i
	case text[i] == '\'' || text[i] == '"':
		quote := text[i]
		for j := i + 1; j < len(text); j++ {
			if text[j] != quote {
				continue
			}
			if j+1 < len(text) && text[j+1] == quote {
				j++ // '' or "" is one quote written twice, not the end of the literal
				continue
			}
			return j + 1 - i
		}
		return len(text) - i
	}
	return 0
}

// blankSQLToken is what one sqlToken leaves in the shape: commentary is put away, and so
// is the content of every value, which is data and not the words of a statement. The
// delimiter stays, because an empty quoted value, an empty escape string and a tag
// written twice are what a value looks like from the outside, and the tag is the only
// part of a dollar-quoted body that is SQL rather than data.
func blankSQLToken(token string) string {
	switch {
	case strings.HasPrefix(token, "--") || strings.HasPrefix(token, "/*"):
		return " "
	case token[0] == '$':
		return dollarTag(token) + dollarTag(token)
	case isEscapeString(token, 0):
		return "e''"
	default:
		return token[:1] + token[:1]
	}
}
