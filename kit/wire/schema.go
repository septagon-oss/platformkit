package wire

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

func enumMove(key, before, after string) string {
	oldSet, ok := enumSet(before)
	if !ok {
		return ""
	}
	newSet, _ := enumSet(after)
	if strings.Contains(key, " request ") {
		for _, value := range oldSet {
			if !slices.Contains(newSet, value) {
				return fmt.Sprintf("B5 (breaking): %s no longer accepts %q, which an older client sends", key, value)
			}
		}
	}
	if strings.Contains(key, " response ") {
		for _, value := range newSet {
			if !slices.Contains(oldSet, value) {
				return fmt.Sprintf("B5 (breaking): %s may now answer %q, which an older reader has no branch for", key, value)
			}
		}
	}
	return ""
}

func enumSet(signature string) ([]string, bool) {
	_, rest, found := strings.Cut(signature, "enum=")
	if !found {
		return nil, false
	}
	return strings.FieldsFunc(rest, func(r rune) bool { return r == ' ' || r == '|' }), true
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
// Values that do not read as one word are quoted, so two declarations cannot share a
// spelling: {"kind":"x permission=y"} is `kind="x permission=y"`, which is not the
// `kind=x permission=y` two members spell. A declaration that is not an object keeps its
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
	return key + "=" + wireValueText(value), true
}

// wireValueText is a declaration value as one segment can hold it: verbatim when it
// reads as one word, quoted when it holds a space, a tab, a newline or a quote, and the
// JSON it is when it is not a string at all.
func wireValueText(value any) string {
	text, isString := value.(string)
	if !isString {
		return wireJSONText(value)
	}
	if strings.ContainsAny(text, " \t\n\"") {
		return strconv.Quote(text)
	}
	return text
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
	if members := wireStringList(node["enum"]); len(members) > 0 {
		slices.Sort(members)
		parts = append(parts, "enum="+strings.Join(members, "|"))
	}
	for _, key := range []string{"minLength", "maxLength", "pattern", "minimum", "maximum"} {
		if value, ok := node[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
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
