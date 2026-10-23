package wire

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// enumMove is B5: a request enum that no longer accepts a member an older client sends, or
// a response enum that may answer with a member an older reader has no branch for. It
// compares whole members, read back from the spelling by wireEnumTokens, so the member a
// diagnostic names is the member that moved.
func enumMove(key, before, after string) string {
	oldSet, ok := wireEnumTokens(before)
	if !ok {
		return ""
	}
	newSet, _ := wireEnumTokens(after)
	if strings.Contains(key, " request ") {
		for _, token := range oldSet {
			if !slices.Contains(newSet, token) {
				return fmt.Sprintf("B5 (breaking): %s no longer accepts %s, which an older client sends", key, wireMemberText(token))
			}
		}
	}
	if strings.Contains(key, " response ") {
		for _, token := range newSet {
			if !slices.Contains(oldSet, token) {
				return fmt.Sprintf("B5 (breaking): %s may now answer %s, which an older reader has no branch for", key, wireMemberText(token))
			}
		}
	}
	return ""
}

// wireEnumTokens reads the enum segment of a shape signature back as the member tokens it
// spells. The segment is one part of the signature: it starts at `enum=` and ends at the
// first space outside a quoted member, so a keyword that follows it is never read as a
// member and a member that holds a space is never read as two.
func wireEnumTokens(signature string) ([]string, bool) {
	rest := signature
	for rest != "" {
		if spelled, found := strings.CutPrefix(rest, "enum="); found {
			return wireSegmentTokens(spelled), true
		}
		end := wireTokenEnd(rest, " ")
		if end >= len(rest) {
			break
		}
		rest = rest[end+1:]
	}
	return nil, false
}

// wireSegmentTokens reads the tokens of one enum segment: `|` continues the segment, and a
// space, or the end of the signature, ends it. An empty token is no member, because the
// writer quotes the empty member as itself.
func wireSegmentTokens(segment string) []string {
	var tokens []string
	for segment != "" {
		end := wireTokenEnd(segment, "| ")
		if token := segment[:end]; token != "" {
			tokens = append(tokens, token)
		}
		if end >= len(segment) || segment[end] != '|' {
			return tokens
		}
		segment = segment[end+1:]
	}
	return tokens
}

// wireTokenEnd returns where the token at the start of text ends: a token opened with a
// quote ends after its unescaped closing quote, and any other token at the first separator
// in cut. text may be empty, which ends nowhere.
func wireTokenEnd(text, cut string) int {
	if text != "" && text[0] == '"' {
		for i := 1; i < len(text); i++ {
			switch text[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
		return len(text)
	}
	if index := strings.IndexAny(text, cut); index >= 0 {
		return index
	}
	return len(text)
}

// wireMemberText is a member token as the JSON text of the member it spells, which is what
// a diagnostic names: a quoted token is the string it holds, a token that opens a JSON
// object, array or number, carries the `=` marker of a number outside the exact window, or
// spells a bool or a null is already that text, and any other token is a string written as
// itself.
func wireMemberText(token string) string {
	if text, err := strconv.Unquote(token); err == nil {
		return strconv.Quote(text)
	}
	if token != "" && (token[0] == '{' || token[0] == '[' || token[0] == '=' || wireReadsAsJSON(token)) {
		return token
	}
	return strconv.Quote(token)
}

// authMembers are the declaration members a reader looks for first, in the order the
// identity spells them: what kit/httpx emits, in the order it emits it. A member that
// is absent, null or empty contributes nothing, so `kind=public` reads the same whether
// the permission was omitted, written as null, or written as "".
var authMembers = []string{"kind", "permission", "feature"}

// wireAuth is one operation's whole authorization declaration, canonically spelled:
// the members named above in that order, then every other member the declaration
// carries, sorted by name. It compares what a door is, never whether the answer is
// sane — a sixth kind, a malformed permission or a feature nobody sells is compared
// here and refused at boot by kit/httpx, which owns what may be declared.
//
// Keys and values that do not read as one member are quoted, so two declarations cannot
// share a spelling: {"kind":"x permission=y"} is `kind="x permission=y"`, which is not the
// `kind=x permission=y` two members spell, and {"a=b":"c"} is `"a=b"=c`, which is not the
// `a=b=c` that {"a":"b=c"} spells — an unquoted key runs to its first `=`, which is why a
// key carrying one cannot be read as a pair. A declaration that is not an object keeps its
// value under `value=`, because a comparator that read it as absent would accept a
// document whose door nobody can name.
func wireAuth(route map[string]any) string {
	declared, present := route["x-platformkit-auth"]
	if !present || declared == nil {
		return ""
	}
	members, isObject := declared.(map[string]any)
	if !isObject {
		return "value=" + wireJSONText(declared)
	}
	var parts []string
	for _, key := range authMembers {
		if part, ok := wireAuthMember(key, members[key]); ok {
			parts = append(parts, part)
		}
	}
	for _, key := range slices.Sorted(maps.Keys(members)) {
		if slices.Contains(authMembers, key) {
			continue
		}
		if part, ok := wireAuthMember(key, members[key]); ok {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}

// wireAuthMember is one member's segment, or nothing when the member says nothing:
// absent, null or empty is what `omitempty` and `Auth.Feature() == ""` already mean.
func wireAuthMember(key string, value any) (string, bool) {
	if value == nil {
		return "", false
	}
	if text, isString := value.(string); isString && text == "" {
		return "", false
	}
	return wireAuthKey(key) + "=" + wireValueText(value), true
}

// wireAuthKey is a declaration member's name as one segment can hold it: verbatim when it
// reads as a name, quoted when it holds a space, a tab, a newline, a quote or an `=`,
// because an unquoted key is the text up to the first `=`. kit/httpx marshals three fixed
// names, so nothing this kernel emits is ever quoted here; a hand-written or third-party
// document can name a member otherwise, and its name has to survive the spelling.
func wireAuthKey(key string) string {
	if strings.ContainsAny(key, " \t\n\"=") {
		return strconv.Quote(key)
	}
	return key
}

// wireValueText is a declaration value as one segment can hold it: verbatim when it
// reads as one word, quoted when it holds a space, a tab, a newline or a quote, the number
// it is at its exact value, and the JSON it is when it is neither.
func wireValueText(value any) string {
	switch typed := value.(type) {
	case string:
		if strings.ContainsAny(typed, " \t\n\"") {
			return strconv.Quote(typed)
		}
		return typed
	case json.Number:
		return wireNumberText(typed)
	default:
		return wireJSONText(value)
	}
}

// wireJSONText is the compact JSON of a declaration member or of a declaration that is
// no object at all. A string keeps its quotes here, which is what keeps
// {"x-platformkit-auth":"public"} from reading as the `value=public` an object holding a
// member named value spells. Every value came out of encoding/json, so it always
// encodes again; the fallback is the text a person reads if that ever stops being true.
func wireJSONText(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return strconv.Quote(fmt.Sprint(value))
	}
	return string(encoded)
}

func walkWire(out map[string]string, id, at string, node map[string]any, doc map[string]any, depth int) {
	if node == nil || depth > 12 {
		return
	}
	key := id + " " + at
	out[key] = wireSignature(node)
	for name, child := range wireMap(node["properties"]) {
		member, _ := child.(map[string]any)
		walkWire(out, id, at+" ."+name, member, doc, depth+1)
	}
	for _, name := range wireStringList(node["required"]) {
		out[key+" requires "+name] = "required"
	}
	if items, ok := node["items"].(map[string]any); ok {
		walkWire(out, id, at+" []", items, doc, depth+1)
	}
	if extra, ok := node["additionalProperties"].(map[string]any); ok {
		walkWire(out, id, at+" {}", extra, doc, depth+1)
	}
	for branch, raw := range wireList(node["anyOf"]) {
		arm, _ := raw.(map[string]any)
		walkWire(out, id, at+" |"+branch, arm, doc, depth+1)
	}
	if ref, _ := node["$ref"].(string); ref != "" {
		if target := wireResolve(doc, ref); target != nil && !strings.Contains(at, "»"+ref+" ") {
			walkWire(out, id, at+" »"+ref, target, doc, depth+1)
		}
	}
}

func wireSignature(node map[string]any) string {
	if node == nil {
		return "none"
	}
	var parts []string
	for _, key := range []string{"type", "format", "$ref"} {
		if value, _ := node[key].(string); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if members, isList := node["enum"].([]any); isList {
		if spelled := wireEnumText(members); spelled != "" {
			parts = append(parts, "enum="+spelled)
		}
	}
	for _, key := range []string{"minLength", "maxLength", "pattern", "minimum", "maximum"} {
		if value, ok := node[key]; ok {
			parts = append(parts, key+"="+wireMemberToken(value))
		}
	}
	if required, _ := node["nullable"].(bool); required {
		parts = append(parts, "nullable")
	}
	return strings.Join(parts, " ")
}

func wireOperationOf(key string) string {
	id, _, _ := strings.Cut(key, " ")
	return id
}

func wireSchema(node map[string]any) map[string]any {
	if node == nil {
		return nil
	}
	if schema, ok := node["schema"].(map[string]any); ok {
		return schema
	}
	if _, ok := node["type"]; ok {
		return node
	}
	return nil
}

func wireMap(node any) map[string]any {
	out, _ := node.(map[string]any)
	if out == nil {
		return map[string]any{}
	}
	return out
}

func wireList(node any) map[string]any {
	list, ok := node.([]any)
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(list))
	for i, item := range list {
		if named, _ := item.(map[string]any); named != nil {
			if name, _ := named["name"].(string); name != "" {
				out[name] = item
				continue
			}
		}
		out[fmt.Sprintf("#%d", i)] = item
	}
	return out
}

func wireStringList(node any) []string {
	list, _ := node.([]any)
	var out []string
	for _, item := range list {
		if value, _ := item.(string); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string { return slices.Sorted(maps.Keys(m)) }
