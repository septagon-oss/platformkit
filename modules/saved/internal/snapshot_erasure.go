package internal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
)

// This file rewrites one person's copies for a personal-data erasure. A snapshot is a
// JSON document kept as the bytes it arrived as, so rewriting it is a document operation:
// this walks the document's own strings, finds the mentions inside the text each one
// decodes to, and puts the placeholder back over the bytes that text arrived as. The
// identifiers stay the kernel's — personaldata.Subject names them and says which is a
// handle — and only the decoded text can be matched without missing a spelling.

// snapshotBatch bounds how many retained documents one pass of the erasure holds in
// memory. Scanning the tenant's copies is the server's job; this is only the driver's.
const snapshotBatch = 200

// mention is one string an erasure stops finding, and the shape a document has to write
// it in for the rewrite to take it.
type mention struct {
	// text is the identifier, as the person is known by it.
	text string

	// login marks a handle, which prose names only as the @-mention it always is.
	login bool
}

// mentions are the subject's own strings, longest first, as a document holds them.
func mentions(s personaldata.Subject) []mention {
	ids := s.Known()
	out := make([]mention, 0, len(ids))
	for _, id := range ids {
		out = append(out, mention{text: id.Value, login: id.Login})
	}
	return out
}

// snapshotRow is the three columns an erasure of a document reads: which copy, whose it
// is, and the document itself.
type snapshotRow struct {
	ID       uuid.UUID `gorm:"column:id"`
	PersonID uuid.UUID `gorm:"column:person_id"`
	Values   string    `gorm:"column:values"`
}

// TableName names the table these rows are read out of.
func (*snapshotRow) TableName() string { return "saved_items" }

// scrubSnapshots rewrites the retained documents that name the subject: every copy the
// tenant can see whose stored document spells one of the subject's identifiers, as the
// identifier stands or as the escaping of a JSON string writes it. A rewritten copy keeps
// its id, its source, its words around the placeholder and the revision every unsave
// quotes, and takes the instant of the rewrite in updated_at, because a rewrite is a
// write. A copy this rewrite cannot read, or whose rewrite would leave bytes which parse as
// nothing, refuses the whole erasure, so an erasure either reaches every document it should
// and commits, or leaves the table as it was.
func scrubSnapshots(ctx context.Context, tx db.Tx[db.Tenant], s personaldata.Subject) error {
	terms := mentions(s)
	spelled, args := spelledIn(s)
	if len(terms) == 0 || len(spelled) == 0 {
		return nil
	}
	placeholders := strings.Join(spelled, " OR ")
	at := db.Now()
	for last := uuid.Nil; ; {
		var held []snapshotRow
		err := tx.DB().WithContext(ctx).Model(&snapshotRow{}).
			Select("id, person_id, values").
			Where("values <> '' AND ("+placeholders+") AND id > ?", append(args[:len(args):len(args)], last)...).
			Order("id").Limit(snapshotBatch).Find(&held).Error
		if err != nil {
			return err
		}
		for i := range held {
			if err = rewriteSnapshot(ctx, tx, &held[i], terms, s, at); err != nil {
				return err
			}
		}
		if len(held) < snapshotBatch {
			return nil
		}
		last = held[len(held)-1].ID
	}
}

// spelledIn is the server's half of the search: the question that asks whether a stored
// document could hold one of the subject's identifiers at all. It asks the stored bytes —
// the identifier as it stands, or with `"` and `\` written the way a document must write
// them — case-insensitively and with no word boundary, because its only job is to keep
// documents out of the driver and out of memory. Decoded text is the rewrite's question,
// and a name the document escaped for its own reasons (\u0057ren) is reached by neither,
// which README §10's Limits sentence names a limit. The escaped spelling is what lets a
// copy holding John \"JJ\" Smith be found for the person called John "JJ" Smith.
func spelledIn(s personaldata.Subject) ([]string, []any) {
	var where []string
	var args []any
	seen := map[string]bool{}
	for _, id := range s.Identifiers() {
		for _, spelling := range []string{id, escapedInJSON(id)} {
			if spelling == "" || seen[spelling] {
				continue
			}
			seen[spelling] = true
			where = append(where, "position(lower(?) in lower(values)) > 0")
			args = append(args, spelling)
		}
	}
	return where, args
}

// escapedInJSON is how a JSON string writes an identifier: the document's serialisation
// is the text the server searches, and a name carrying a quotation mark or a backslash is
// not the same bytes decoded and re-encoded. Only the two characters JSON must escape are
// rewritten, which is the widening this filter asks for and nothing more.
func escapedInJSON(id string) string {
	if !strings.ContainsAny(id, `"\`) {
		return id
	}
	var out strings.Builder
	out.Grow(len(id) + 8)
	for _, r := range id {
		switch r {
		case '"':
			out.WriteString(`\"`)
		case '\\':
			out.WriteString(`\\`)
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// rewriteSnapshot writes one document back, if the erasure changes it at all. A copy that
// this rewrite cannot read, or that no longer answers as a document after its rewrite, is a
// refusal rather than a write: the transaction holding the erasure rolls back, and no copy
// is left holding bytes every reader of it would choke on. A row selected for the subject
// and then left unread is refused for the same reason — an erasure that reported the copy
// done would leave the person's own words standing in it forever, unremarked.
func rewriteSnapshot(ctx context.Context, tx db.Tx[db.Tenant], held *snapshotRow,
	terms []mention, s personaldata.Subject, at time.Time) error {
	// The copy a person kept of their own source is deleted outright before this runs, so
	// every row still standing is one the subject did not save. The bare-word form of a
	// handle, which the kernel reserves for a row carrying its subject's own id, is offered
	// to the row that ever says so, and the @-mention is all another person's copy loses.
	next, changed, err := rewriteDocument(held.Values, terms, s.Token(), held.PersonID == s.ID)
	if err != nil {
		return fmt.Errorf("saved: erasing %s could not read the copy %s: %w", s.ID, held.ID, err)
	}
	if !changed {
		return nil
	}
	if !json.Valid([]byte(next)) {
		return fmt.Errorf("saved: erasing %s would leave the copy %s holding bytes that parse as nothing", s.ID, held.ID)
	}
	return tx.DB().WithContext(ctx).Model(&snapshotRow{}).Where("id = ?", held.ID).
		Updates(map[string]any{"values": next, "updated_at": at}).Error
}

// rewriteDocument answers document with the same document, its strings written with no
// mention of the subject left in them, and whether it changed anything. A replacement
// made over the document's bytes can begin one byte inside an escape the document wrote
// to hold a quotation mark, and then leaves bytes that parse as nothing: the copy would
// answer no read, take its whole page of the list down with it, and fail the export of the
// person who only kept it. So the match is made on the text each string decodes to, and
// the placeholder is spliced over the bytes that text arrived as, whole escape sequences
// included — which also reaches a name the escaping wrote, the one a match over stored
// bytes leaves standing. Untouched strings, numbers and key order come back byte for byte.
// A document this rewrite cannot read — bytes that parse as no document at all, or a string
// whose escapes no reader of the column could decode — answers an error rather than itself:
// the copy is one the erasure cannot certify, and answering with the document unchanged is
// how an erasure comes to report a person forgotten while their name still stands.
func rewriteDocument(document string, terms []mention, token string, own bool) (string, bool, error) {
	if len(terms) == 0 {
		return document, false, nil
	}
	if !json.Valid([]byte(document)) {
		return document, false, errors.New("the copy holds bytes that parse as no document")
	}
	var out strings.Builder
	out.Grow(len(document))
	rewrote := false
	for at := 0; at < len(document); {
		if document[at] != '"' {
			out.WriteByte(document[at])
			at++
			continue
		}
		held, ok := decodeString(document, at+1)
		if !ok {
			return document, false, errors.New("the copy holds a string no reader of it can decode")
		}
		if kept, changed := held.splice(terms, token, own); changed {
			out.WriteString(kept)
			rewrote = true
		} else {
			out.WriteString(document[at : held.end+1])
		}
		at = held.end + 1
	}
	if !rewrote {
		return document, false, nil
	}
	return out.String(), true, nil
}

// documentString is one string of a document: the text it decodes to, where its closing
// quote sits, and, for each byte of that text, the document bytes it arrived as. Text and
// provenance are kept together because a replacement is spliced over the bytes its text
// arrived as, which is what keeps a rewrite from splitting an escape sequence in half.
type documentString struct {
	doc       string
	text      string
	from, end int
	lo, hi    []int
}

// decodeString reads the string of document that begins at from — the byte after its
// opening quote — and answers the text it holds. A string that never closes, or that
// carries an escape the JSON grammar does not write at all, answers false, and the caller
// refuses the whole document rather than rewriting part of it.
func decodeString(document string, from int) (documentString, bool) {
	held := documentString{doc: document, from: from}
	for at := from; at < len(document); {
		switch document[at] {
		case '"':
			held.end = at
			return held, true
		case '\\':
			text, width, ok := unescape(document[at:])
			if !ok {
				return documentString{}, false
			}
			held.take(text, at, at+width)
			at += width
		default:
			held.take(document[at:at+1], at, at+1)
			at++
		}
	}
	return documentString{}, false
}

// take adds decoded text and the one document range it was written as, repeated over the
// bytes that text arrived in so every byte of it can be traced back.
func (held *documentString) take(text string, lo, hi int) {
	held.text += text
	for range len(text) {
		held.lo = append(held.lo, lo)
		held.hi = append(held.hi, hi)
	}
}

// unescape reads the escape sequence at the head of at, and answers the text it stands for
// and the width of the sequence that stood for it. A surrogate names a character only in
// its pair, and only a high one begins one; a lone one stands for the replacement character
// over the width of the escape that wrote it, which is what every other reader of this
// column makes of it — encoding/json, and behind it Get, List, the export and the copy's
// own keeper. A document may hold one and still be a document: the acceptance rule at Save
// is a valid UTF-8 JSON object, RFC 8259 admits the escape, and the column is text, so this
// read does not refuse what its readers read. Only an escape the grammar does not write at
// all answers false.
func unescape(at string) (string, int, bool) {
	if len(at) < 2 {
		return "", 0, false
	}
	switch at[1] {
	case '"', '\\', '/':
		return at[1:2], 2, true
	case 'b':
		return "\b", 2, true
	case 'f':
		return "\f", 2, true
	case 'n':
		return "\n", 2, true
	case 'r':
		return "\r", 2, true
	case 't':
		return "\t", 2, true
	case 'u':
		first, ok := hex4(at[2:])
		if !ok {
			return "", 0, false
		}
		if first < 0xD800 || first > 0xDFFF {
			return string(rune(first)), 6, true
		}
		if utf16IsLow(first) {
			return string(utf8.RuneError), 6, true
		}
		second, ok := hex4(at[8:])
		if !ok || !utf16IsLow(second) {
			return string(utf8.RuneError), 6, true
		}
		return string(utf16.Decode([]uint16{uint16(first), uint16(second)})), 12, true
	}
	return "", 0, false
}

func utf16IsLow(r rune) bool { return r >= 0xDC00 && r <= 0xDFFF }

// hex4 reads the four hexadecimal digits of a \u escape as one UTF-16 code unit.
func hex4(at string) (rune, bool) {
	if len(at) < 4 {
		return 0, false
	}
	var r rune
	for _, c := range at[:4] {
		r <<= 4
		switch {
		case c >= '0' && c <= '9':
			r |= rune(c - '0')
		case c >= 'a' && c <= 'f':
			r |= rune(c-'a') + 10
		case c >= 'A' && c <= 'F':
			r |= rune(c-'A') + 10
		default:
			return 0, false
		}
	}
	return r, true
}

// span is a run of one string's decoded text that the erasure replaces, in the decoded
// text's own bytes.
type span struct{ from, to int }

// splice answers the string as the document should hold it afterwards: its own bytes, with
// every span its text matches replaced by token. A string no term matches comes back
// exactly as it stood, escape spellings included.
func (held documentString) splice(terms []mention, token string, own bool) (string, bool) {
	spans := matched(held.text, terms, own)
	if len(spans) == 0 {
		return "", false
	}
	var out strings.Builder
	out.Grow(len(held.text) + len(token)*len(spans))
	out.WriteByte('"')
	copied := held.from
	for _, s := range spans {
		out.WriteString(held.kept(copied, held.lo[s.from]))
		out.WriteString(token)
		copied = held.hi[s.to-1]
	}
	out.WriteString(held.kept(copied, held.end))
	out.WriteByte('"')
	return out.String(), true
}

// kept is what the erasure leaves alone: the document's own bytes from wherever the copy
// reached to, up to the byte the next replacement begins at. The ranges a string's text
// arrived as are ordered and never overlap, so this can neither skip nor repeat a byte.
func (held documentString) kept(from, to int) string {
	if to <= from {
		return ""
	}
	return held.doc[from:to]
}

// matched finds the spans of a string's text an erasure replaces, leftmost first and,
// where two identifiers could both begin at one place, the longer, which is the order
// Subject.Known already puts them in. The placeholder is never searched again, so a person
// whose name is a word the placeholder spells — <erased 1a2b3c4d> holds the word erased,
// and any eight hex digits — cannot have the placeholder rewritten out from under itself.
func matched(text string, terms []mention, own bool) []span {
	var out []span
	for at := 0; at < len(text); {
		if to, ok := stands(text, at, terms, own); ok {
			out = append(out, span{from: at, to: to})
			at = to
			continue
		}
		_, n := utf8.DecodeRuneInString(text[at:])
		at += n
	}
	return out
}

// stands reports whether one of the subject's identifiers names them at this place in
// text, and where the span that means it ends. A handle stands as the @-mention written in
// front of it, whose @ the erasure takes with it, and as the bare word only in a copy the
// subject kept themselves; an address and a name stand wherever the words around them let
// a word begin and end.
func stands(text string, at int, terms []mention, own bool) (int, bool) {
	for _, term := range terms {
		if to, ok := term.stands(text, at, own); ok {
			return to, true
		}
	}
	return 0, false
}

func (term mention) stands(text string, at int, own bool) (int, bool) {
	first, _ := utf8.DecodeRuneInString(term.text)
	last, _ := utf8.DecodeLastRuneInString(term.text)
	if term.login {
		if to, ok := mentionAt(text, at, term.text, last); ok {
			return to, true
		}
		if !own {
			return 0, false
		}
	}
	if !beginsWord(text, at, first) {
		return 0, false
	}
	to, ok := foldAt(text, term.text, at)
	if !ok || !endsWord(text, to, last) {
		return 0, false
	}
	return to, true
}

// mentionAt reads the one spelling of a handle that prose cannot be mistaken for: the
// mention, @ included. The closing refusal is the kernel's — "@will.co" is somebody else's
// mailhost, and rewriting it would destroy a third party's words to forget one person.
func mentionAt(text string, at int, handle string, last rune) (int, bool) {
	if at >= len(text) || text[at] != '@' {
		return 0, false
	}
	to, ok := foldAt(text, handle, at+1)
	if !ok || !endsWord(text, to, last) {
		return 0, false
	}
	if to+1 < len(text) && text[to] == '.' {
		if next, _ := utf8.DecodeRuneInString(text[to+1:]); unicode.IsLetter(next) || unicode.IsDigit(next) {
			return 0, false
		}
	}
	return to, true
}

// foldAt reports whether term's runes stand at this place in text, read case-insensitively
// the way the server's match reads them, and answers the byte just past them. It compares
// rune by rune rather than lower-casing either side: lower-casing some runes changes their
// length, and a span found in lowered text would splice the wrong bytes out of the
// document.
func foldAt(text, term string, at int) (int, bool) {
	for t := 0; t < len(term); {
		if at >= len(text) {
			return 0, false
		}
		r, n := utf8.DecodeRuneInString(text[at:])
		w, m := utf8.DecodeRuneInString(term[t:])
		if unicode.ToLower(r) != unicode.ToLower(w) {
			return 0, false
		}
		at += n
		t += m
	}
	return at, true
}

// beginsWord and endsWord are the server's word boundaries, \m and \M: written only beside
// a character of the identifier's own that the language counts as a word character, and
// satisfied only by a neighbour that is not one. Without them, erasing a person called Ann
// rewrites a colleague's "Annual planning", which is data that belongs to somebody else and
// an operation nobody can undo.
func beginsWord(text string, at int, first rune) bool {
	if !isWordRune(first) || at == 0 {
		return true
	}
	before, _ := utf8.DecodeLastRuneInString(text[:at])
	return !isWordRune(before)
}

func endsWord(text string, at int, last rune) bool {
	if !isWordRune(last) || at >= len(text) {
		return true
	}
	next, _ := utf8.DecodeRuneInString(text[at:])
	return !isWordRune(next)
}

// isWordRune is the class the server decides a word boundary against: letters and digits of
// any script, and the underscore.
func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}
