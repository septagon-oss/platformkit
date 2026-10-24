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
// member and a member that holds a space is never read as two. Every part it steps over is
// stepped over whole — see wirePartEnd — so a value quoted to hold a space of its own, a
// pattern of `x enum=p`, cannot hand the scan an enum segment no shape declares.
func wireEnumTokens(signature string) ([]string, bool) {
	rest := signature
	for rest != "" {
		if spelled, found := strings.CutPrefix(rest, "enum="); found {
			return wireSegmentTokens(spelled), true
		}
		end := wirePartEnd(rest)
		if end >= len(rest) {
			break
		}
		rest = rest[end+1:]
	}
	return nil, false
}

// wirePartEnd is where the `key=value` part at the start of text ends: the first space no
// quoted value covers. A part whose value holds a space is the one the writer quotes, so a
// space inside a quote separates nothing, and because a quoted value escapes its own
// backslashes, `\"` is content rather than the close of a value.
func wirePartEnd(text string) int {
	quoted := false
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\\':
			if quoted {
				i++
			}
		case '"':
			quoted = !quoted
		case ' ':
			if !quoted {
				return i
			}
		}
	}
	return len(text)
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

// One comparison reads a document to its end or says where it stopped. Two limits bound that
// reading, and each names itself in the refusal beside the address it gave up on: the depth
// of one schema path, and the number of schema nodes one document's traversal may visit.
// Depth alone bounds neither the work nor the memory: a component several operations reach is
// expanded once per path that reaches it, and every record the walk writes embeds the whole
// expanded path. Neither limit is an argument — a caller that could raise its own could
// certify what it never read — so raising one is a change to this package, beside the
// measured depth and node counts of the reference composition.
const (
	walkDepthLimit = 24
	walkNodeBudget = 200_000
)

// Why an analysis stopped. A stop is never a claim that a declaration moved: no pair of
// records exists at the address it names, on the side it names.
const (
	stopDepth   = "depth"
	stopKeyword = "keyword"
	stopBudget  = "budget"
)

// wireAnalysis is one document's whole traversal: the shape record of every node the walk
// read, and every node it gave up on with the reason.
type wireAnalysis struct {
	shapes map[string]string
	stops  []wireStop
}

// wireStop is the address an analysis gave up on, and what made it give up.
type wireStop struct {
	kind, key, words string
}

// refusal states the incomplete verdict beside the side whose document stopped being read.
// Its prefix differs from every B1–B6 diagnostic on purpose: those two documents were read
// and compared, and this one was not.
func (s wireStop) refusal(side string) string {
	switch s.kind {
	case stopDepth:
		return fmt.Sprintf("B7 (incomplete): %s sits past the traversal limit of %d schema levels in the %s document; the shapes below it were not compared", s.key, walkDepthLimit, side)
	case stopBudget:
		return fmt.Sprintf("B7 (incomplete): the analysis stopped after %d schema nodes in the %s document; further shapes at %s and elsewhere were not compared", walkNodeBudget, side, s.key)
	default:
		return fmt.Sprintf("B7 (incomplete): %s declares %s, which this gate does not read; the shapes it constrains were not compared in the %s document", s.key, s.words, side)
	}
}

// wireWalk is one document's traversal. It carries the records, the budget spent, and where
// it stopped, because a comparison that ran out of either has to say so through the result
// rather than returning the empty answer a caller reads as "compatible".
type wireWalk struct {
	doc         map[string]any
	shapes      map[string]string
	stops       []wireStop
	named       map[string]bool
	spent       int
	outOfBudget string
}

func newWireWalk(doc map[string]any) *wireWalk {
	return &wireWalk{doc: doc, shapes: map[string]string{}, named: map[string]bool{}}
}

// analysis is the whole traversal: the stops in the order the walk reached them, the one
// budget refusal last, since the budget ends the walk wherever it happens to stand.
func (w *wireWalk) analysis() wireAnalysis {
	if w.outOfBudget != "" {
		w.stops = append(w.stops, wireStop{kind: stopBudget, key: w.outOfBudget})
	}
	return wireAnalysis{shapes: w.shapes, stops: w.stops}
}

// stop names one address the walk gave up on, once per address and reason: a shape past the
// limit has children the walk never reaches, so it cannot report a stop for each of them too.
func (w *wireWalk) stop(kind, key, words string) {
	if mark := kind + " " + key + " " + words; !w.named[mark] {
		w.named[mark] = true
		w.stops = append(w.stops, wireStop{kind: kind, key: key, words: words})
	}
}

// walk records the shape of the schema node at address `at`, then descends the children the
// profile reads in a fixed order — properties by name, requirement records, items, the open
// slot, anyOf as written, a reference last — so two runs of one comparison reach the same
// address when they stop, and the address they name is a fact a reviewer can check.
func (w *wireWalk) walk(id, at string, node map[string]any, depth int) {
	if node == nil {
		return
	}
	key := id + " " + at
	if w.spent >= walkNodeBudget {
		if w.outOfBudget == "" {
			w.outOfBudget = key
		}
		return
	}
	if depth > walkDepthLimit {
		w.stop(stopDepth, key, "")
		return
	}
	w.spent++
	w.shapes[key] = wireSignature(node)
	if unread := wireUnreadKeywords(node); len(unread) > 0 {
		w.stop(stopKeyword, key, strings.Join(unread, ", "))
	}
	for _, name := range slices.Sorted(maps.Keys(wireMap(node["properties"]))) {
		child, _ := wireMap(node["properties"])[name].(map[string]any)
		w.walk(id, at+" ."+name, child, depth+1)
	}
	for _, name := range wireStringList(node["required"]) {
		w.shapes[key+" requires "+name] = "required"
	}
	if items, ok := node["items"].(map[string]any); ok {
		w.walk(id, at+" []", items, depth+1)
	}
	w.walkOpen(id, at, node, depth)
	if arms, isList := node["anyOf"].([]any); isList {
		for index, raw := range arms {
			arm, _ := raw.(map[string]any)
			w.walk(id, at+" |"+wireArmName(arm, index), arm, depth+1)
		}
	}
	if ref, _ := node["$ref"].(string); ref != "" {
		if target := wireResolve(w.doc, ref); target != nil && !strings.Contains(at, "»"+ref+" ") {
			w.walk(id, at+" »"+ref, target, depth+1)
		}
	}
}

// walkOpen descends the properties a node does not name. An open slot is recorded as the
// empty schema it is, whether the document wrote `true` or wrote a schema, so the two
// spellings of "anything may follow" are one contract; only `false` says something the node's
// own signature does not already say, and wireSignature carries that.
func (w *wireWalk) walkOpen(id, at string, node map[string]any, depth int) {
	switch extra := node["additionalProperties"].(type) {
	case map[string]any:
		w.walk(id, at+" {}", extra, depth+1)
	case bool:
		if extra {
			w.walk(id, at+" {}", map[string]any{}, depth+1)
		}
	}
}

// wireArmName is the address segment an anyOf branch contributes: the name it declares, as
// wireList names it, or the position it was written at.
func wireArmName(arm map[string]any, index int) string {
	if name, _ := arm["name"].(string); name != "" {
		return name
	}
	return "#" + strconv.Itoa(index)
}

// wireReadKeywords are the schema keywords this comparison records or descends. A node that
// declares only these has had its whole shape read.
var wireReadKeywords = []string{
	"$ref", "additionalProperties", "anyOf", "enum", "format", "items", "maxItems",
	"maxLength", "maximum", "minItems", "minLength", "minimum", "nullable", "pattern",
	"properties", "required", "type",
}

// wireDescriptiveKeywords say nothing about which instances satisfy a schema: they name it,
// document it, carry a sample or frame a dialect. Ignoring one cannot misread a contract, so
// a node declaring only these is read in full. An extension is descriptive by its `x-`
// prefix, whatever it is named.
var wireDescriptiveKeywords = []string{
	"$anchor", "$comment", "$defs", "$dynamicAnchor", "$dynamicRef", "$id", "$schema",
	"contentEncoding", "contentMediaType", "default", "deprecated", "description",
	"discriminator", "example", "examples", "externalDocs", "readOnly", "title", "writeOnly",
	"xml",
}

// wireUnreadKeywords names, in the order a reader checks them, the constraints on a node
// that this gate neither reads nor can dismiss as prose. The profile is a whitelist rather
// than a list of the shapes known to be trouble, so a keyword a publisher starts writing
// refuses the pair rather than passing it unseen.
func wireUnreadKeywords(node map[string]any) []string {
	var unread []string
	for _, key := range slices.Sorted(maps.Keys(node)) {
		if strings.HasPrefix(key, "x-") || slices.Contains(wireReadKeywords, key) || slices.Contains(wireDescriptiveKeywords, key) {
			continue
		}
		unread = append(unread, key)
	}
	return unread
}

// wireTypeToken is a type name as one element of a shape: written as itself while it reads
// back as exactly one name, quoted when it holds a separator, a bracket, a quote or a
// backslash the set's own brackets rely on. A name is read as a name here, so `null` is the
// name JSON Schema gives the null type and stays one word unquoted.
func wireTypeToken(name string) string {
	if name == "" || strings.ContainsAny(name, " \t\n\"|[]\\=") {
		return strconv.Quote(name)
	}
	return name
}

// wireSignature is a node's shape as one text: the type it answers, the format and the
// reference it names, the members it accepts, the bounds it states, whether null answers, and
// whether anything the node does not name may answer. Two documents that say the same thing
// spell the same text; a part is written only where the document says something.
func wireSignature(node map[string]any) string {
	if node == nil {
		return "none"
	}
	var parts []string
	if part := wireTypePart(node["type"]); part != "" {
		parts = append(parts, part)
	}
	for _, key := range []string{"format", "$ref"} {
		if value, _ := node[key].(string); value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	if members, isList := node["enum"].([]any); isList {
		if spelled := wireEnumText(members); spelled != "" {
			parts = append(parts, "enum="+spelled)
		}
	}
	for _, key := range []string{"minLength", "maxLength", "minItems", "maxItems", "pattern", "minimum", "maximum"} {
		if value, ok := node[key]; ok {
			parts = append(parts, key+"="+wireMemberToken(value))
		}
	}
	if required, _ := node["nullable"].(bool); required {
		parts = append(parts, "nullable")
	}
	if stated, isBool := node["additionalProperties"].(bool); isBool && !stated {
		parts = append(parts, "additionalProperties=false")
	}
	return strings.Join(parts, " ")
}

// wireTypePart spells the `type=` part: one name as itself, a set of names as that set —
// sorted, deduplicated, bracketed, each name spelled as one member — and nothing where the
// document names no type. A set of exactly one is that one name, so writing it as a set says
// nothing. validateSchema refuses a type that names nothing before this is ever reached; the
// document's own JSON is what remains to compare if some other path brings one here.
func wireTypePart(value any) string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return ""
		}
		return "type=" + wireTypeToken(typed)
	case []any:
		names := make([]string, 0, len(typed))
		for _, item := range typed {
			if name, _ := item.(string); name != "" {
				names = append(names, wireTypeToken(name))
			}
		}
		slices.Sort(names)
		names = slices.Compact(names)
		if len(names) == 0 {
			return "type=" + wireJSONToken(typed)
		}
		if len(names) == 1 {
			return "type=" + names[0]
		}
		return "type=[" + strings.Join(names, "|") + "]"
	case nil:
		return ""
	}
	return "type=" + wireMemberToken(value)
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
