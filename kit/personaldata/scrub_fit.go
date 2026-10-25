package personaldata

import (
	"strings"
)

// fits uses the wide rewrite, then the narrow rewrite, then a safe shortening,
// whichever fits the column's bounds. Subqueries evaluate each rewrite once;
// character limits apply before byte limits. mark is the fallback placeholder.
func fits(column string, room extent, wide, narrow, mark piece) piece {
	// standsInside is the test that the named value of the FROM below keeps every bound the
	// column carries, and the values those two measures are measured against, kept beside the
	// text that asks for them. It always names at least one measure: fits is reached only for a
	// column whose extent answered with a room at all. See bounds.
	standsInside := func(name string) piece {
		var tests []string
		var values []any
		if room.chars > 0 {
			tests = append(tests, "length("+name+") <= ?")
			values = append(values, room.chars)
		}
		if room.octets > 0 {
			tests = append(tests, "octet_length("+name+") <= ?")
			values = append(values, room.octets)
		}
		return piece{sql: strings.Join(tests, " AND "), args: values}
	}
	// downToCharacters is the named value cut to the character bound, which is the cut the byte
	// bound is then counted over. See byteRoom.
	downToCharacters := func(name string) piece {
		if room.chars == 0 {
			return piece{sql: name}
		}
		return piece{sql: "left(" + name + ", ?)", args: []any{room.chars}}
	}
	return bound(
		piece{sql: column + " = (SELECT CASE WHEN "}, standsInside("room.wide"),
		piece{sql: " THEN room.wide WHEN "}, standsInside("room.narrow"),
		piece{sql: " THEN room.narrow ELSE "}, shortest(room),
		piece{sql: " END FROM (SELECT raw.wide AS wide, raw.narrow AS narrow, "},
		downToCharacters("raw.narrow"), piece{sql: " AS cut, "},
		downToCharacters("raw.mark"), piece{sql: " AS mark FROM (SELECT "}, wide,
		piece{sql: " AS wide, "}, narrow, piece{sql: " AS narrow, "}, mark,
		piece{sql: " AS mark) AS raw) AS room)"},
	)
}

// shortest is the value to write when neither placeholder fits: the cut the room already took,
// taken down to the byte bound too where the column carries one, and then taken back to the last
// character that could not have stood inside one of the person's identifiers. That last step is
// what keeps a shortened value anonymous; see outsideWord.
//
// Where even that leaves nothing — a value that is one unbroken word the whole way to its
// ceiling — the column takes the placeholder rather than an empty string, so the row still says
// an erased person sat in it and a product that holds its column to a minimum length finds its
// erasure refused by nothing.
func shortest(room extent) piece {
	return bound(
		piece{sql: "coalesce(nullif("}, outsideWord(byteRoom("room.cut", room)),
		piece{sql: ", ''), "}, byteRoom("room.mark", room), piece{sql: ")"},
	)
}

// outsideWord removes trailing identifier characters from a shortened value
// so a cut cannot turn a longer, unrelated word back into the erased identifier.
func outsideWord(value piece) piece {
	return bound(piece{sql: "regexp_replace("}, value,
		piece{sql: ", ?, '', 'g')", args: []any{identifierRun}})
}

// identifierRun follows Postgres word characters and includes dots because
// shortening an @handle.domain must not turn it into a bare handle mention.
const identifierRun = `[[:alnum:]_.]+$`

// byteRoom takes the characters a byte bound finds over the bound, counting them off the tail of
// the named column of fits's sub-select. It measures characters rather than bytes because these
// are text columns and a character is what a reader loses: dropping a character drops at least
// one byte, so dropping as many characters as there are bytes over the bound is the smallest cut
// that is always enough, and the only one that never touches a character the row was allowed to
// keep. Where the column bounds characters alone, the named value is already inside the room.
func byteRoom(name string, room extent) piece {
	value := piece{sql: name}
	if room.octets == 0 {
		return value
	}
	return bound(piece{sql: "left("}, value,
		piece{sql: ", greatest(length("}, value,
		piece{sql: ") - greatest(octet_length("}, value,
		piece{sql: ") - ?, 0), 0))", args: []any{room.octets}})
}
