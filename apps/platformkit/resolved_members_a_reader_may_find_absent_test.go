package main

// A resolved policy states what an entry declares. Where it declares nothing the
// resolution prints nothing: no `confirmation` for a command that asks nothing first,
// no `deck` for an entry that cannot be swiped, no `money` reading for a field that
// holds no amount. The published document used to call those members required and name
// no null, so a client generated from it refused the body the kernel serves at the very
// address a shell is generated from.
//
// Each reviewed record below moves one member of one node of one operation out of the
// document's `required`, spelled as the refusal spells it, and forgives nothing else:
// the member stays a declared property naming the component it always named, so every
// body a reader admitted before the move is admitted after it. What a reader never
// admitted is a null in one of these places, and that is what it still cannot admit.

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	wiregate "github.com/septagon-oss/platformkit/kit/wire"
)

// resolvedAddress reaches the resolved object of one entry through one media type of the
// published document: the entry's `resolved` member, one component deep, as the
// comparison addresses a node.
func resolvedAddress(media string) string {
	return "app-resources response 200 " + media +
		" »#/components/schemas/Catalog .resources []" +
		" »#/components/schemas/Entry .resolved »#/components/schemas/Resolved"
}

// resolvedMedia are the two spellings that response is answered in: the JSON a generated
// client reads, and the exact-int64 spelling for a client that cannot hold a 64-bit
// integer. A document names its own `required` for every node, so each document is
// reviewed against the list of every spelling that document publishes.
const (
	resolvedMediaJSON  = "application/json"
	resolvedMediaInt64 = "application/vnd.platformkit.int64+json"
)

// The nodes below the resolved object that publish one of its members, spelled as the
// comparison addresses them, with the [] of a list. A resolved field is published twice —
// the entry's own list, and the fields a command asks for — and an option sits below each
// of those and below the list's filters.
const (
	resolvedIdentityNode     = " .identity »#/components/schemas/ResolvedIdentity"
	resolvedSummaryNode      = " .summary »#/components/schemas/ResolvedSummary"
	resolvedCommandNode      = " .commands [] »#/components/schemas/ResolvedCommand"
	resolvedFieldNode        = " .fields [] »#/components/schemas/ResolvedField"
	resolvedCommandFieldNode = resolvedCommandNode + resolvedFieldNode
	resolvedReadingNode      = " .reading »#/components/schemas/ResolvedReading"
	resolvedOptionNode       = " .options [] »#/components/schemas/ResolvedOption"
	resolvedFilterOptionNode = " .list »#/components/schemas/ResolvedList" +
		" .filters [] »#/components/schemas/ResolvedFilter" +
		" .values [] »#/components/schemas/ResolvedOption"
)

// wireRequirednessAllowances are the reviewed moves of the composition's resolved members
// from required to optional, one record per member and per node that publishes it, at the
// one media spelling a document answers in — the reference document holds both at once.
func wireRequirednessAllowances(media string) []wiregate.RequirednessAllowance {
	const reviewed = "2026-10-25"
	none := func(what string) string {
		return "The resolution prints no " + what + "; a client generated from the document refused the body the kernel serves"
	}
	// One row per member and the node that publishes it: the record is that node's address
	// plus ` requires <member>`, which is how the comparison names the requirement.
	moves := []struct{ node, member, declares string }{
		{"", "deck", "deck for an entry that declares no swipe deck"},
		{"", "savable", "saving declaration for an entry that is not savable"},
		{resolvedIdentityNode, "group", "group for an entry outside navigation"},
		{resolvedSummaryNode, "highlight", "highlight for a card with no declared sub-count"},
		{resolvedCommandNode, "confirmation", "confirmation for a command that asks nothing first"},
		{resolvedFieldNode, "choice", "choice for a field that offers no answers"},
		{resolvedCommandFieldNode, "choice", "choice for a field that offers no answers"},
		{resolvedFieldNode + " .choice »#/components/schemas/ResolvedChoice", "maximum", "maximum for a choice that caps nothing"},
		{resolvedCommandFieldNode + " .choice »#/components/schemas/ResolvedChoice", "maximum", "maximum for a choice that caps nothing"},
		{resolvedFieldNode, "reference", "reference for a field whose author guessed no relationship"},
		{resolvedCommandFieldNode, "reference", "reference for a field whose author guessed no relationship"},
		{resolvedFieldNode + resolvedReadingNode, "money", "money reading for a field that holds no amount"},
		{resolvedCommandFieldNode + resolvedReadingNode, "money", "money reading for a field that holds no amount"},
		{resolvedFieldNode + resolvedReadingNode, "time", "time reading for a field that holds no date"},
		{resolvedCommandFieldNode + resolvedReadingNode, "time", "time reading for a field that holds no date"},
		{resolvedFieldNode + resolvedOptionNode, "picture", "picture for an option drawn with no artwork"},
		{resolvedCommandFieldNode + resolvedOptionNode, "picture", "picture for an option drawn with no artwork"},
		{resolvedFilterOptionNode, "picture", "picture for an option drawn with no artwork"},
	}
	out := make([]wiregate.RequirednessAllowance, 0, len(moves))
	for _, move := range moves {
		out = append(out, wiregate.RequirednessAllowance{
			Record:     resolvedAddress(media) + move.node + " requires " + move.member,
			ReviewedOn: reviewed,
			Reason:     none(move.declares),
		})
	}
	return out
}

// resolvedOptionalMembers are the members a reader of the resolved object may find
// absent: the path down to the member, and the component that declares it. The records
// above move these, and the case below asks the served document about them.
var resolvedOptionalMembers = []struct {
	path      []string
	component string
}{
	{[]string{"deck"}, "Resolved"},
	{[]string{"identity", "group"}, "ResolvedIdentity"},
	{[]string{"fields", "reading", "money"}, "ResolvedReading"},
	{[]string{"fields", "reading", "time"}, "ResolvedReading"},
	{[]string{"fields", "reference"}, "ResolvedField"},
	{[]string{"commands", "confirmation"}, "ResolvedCommand"},
	{[]string{"summary", "highlight"}, "ResolvedSummary"},
}

// deviceContract is the reference composition's two answers, read from one running
// process: the document a client is generated from, and the body that client reads.
type deviceContract struct {
	doc     map[string]any
	catalog []byte
}

// serveDeviceContract boots the reference composition the way the product boots it and
// reads both halves of the device contract from it.
func serveDeviceContract(t *testing.T) deviceContract {
	t.Helper()
	cfg, mods, fixture := deviceComposition(t)
	install(t, fixture.path)
	start(t, cfg, mods, fixture.opts)
	_, document := do(t, cfg, nil, http.MethodGet, acmeHost, "/openapi.json", "")
	_, resources := do(t, cfg, signIn(t, cfg, acmeHost, adminEmail, adminPass), http.MethodGet, acmeHost, "/api/v1/app/resources", "")
	return deviceContract{doc: wireDocument(t, []byte(document)), catalog: []byte(resources)}
}

// component is one published component of the served document.
func (c deviceContract) component(t *testing.T, name string) map[string]any {
	t.Helper()
	component := wireMap(wireMap(wireMap(c.doc["components"])["schemas"])[name])
	if len(component) == 0 {
		t.Fatalf("the served document publishes no %s", name)
	}
	return component
}

// property is where a declared member of a component is published.
func (c deviceContract) property(t *testing.T, component, member string) map[string]any {
	t.Helper()
	published, declared := wireMap(c.component(t, component)["properties"])[member]
	if !declared {
		t.Fatalf("%s publishes no %q", component, member)
	}
	return wireMap(published)
}

// resolvedPolicies are the resolved objects the composition serves, one per entry that
// carries one.
func (c deviceContract) resolvedPolicies(t *testing.T) []map[string]any {
	t.Helper()
	var policies []map[string]any
	for _, entry := range c.entries(t) {
		if resolved := wireMap(entry["resolved"]); len(resolved) > 0 {
			policies = append(policies, resolved)
		}
	}
	return policies
}

// resolvedTestbed is one served resolved policy, re-read each time so a check below owns
// the only difference between the shapes it asks about. With a path it answers an entry
// that prints something above that member, so a list is not asked where it holds none.
func (c deviceContract) resolvedTestbed(t *testing.T, path ...string) map[string]any {
	t.Helper()
	for _, policy := range c.resolvedPolicies(t) {
		if len(wireMap(policy["identity"])) > 0 && len(wireSlice(policy["fields"])) > 0 && len(resolvedHolders(policy, path)) > 0 {
			return policy
		}
	}
	t.Fatalf("no served resolved policy carries an identity, a field and something above %v, so this case would say nothing", path)
	return nil
}

func (c deviceContract) entries(t *testing.T) []map[string]any {
	t.Helper()
	var catalog struct {
		Resources []map[string]any `json:"resources"`
	}
	if err := json.Unmarshal(c.catalog, &catalog); err != nil {
		t.Fatalf("the served catalog is not JSON: %v", err)
	}
	var entries []map[string]any
	for _, entry := range catalog.Resources {
		if entry != nil {
			entries = append(entries, entry)
		}
	}
	return entries
}

// TestTheServedResolvedPolicyAnswersTheServedDocument asks the running composition both
// halves of one promise, in the bytes it answers with rather than the Go tags that
// produced them. A member the resolution may leave undeclared is still a declared
// property of its component, published as the component it always named, and no longer
// demanded by it. Every resolved policy the composition serves answers that document, so
// the producer prints no null where the document never admitted one. Each member is then
// asked about in the shapes a reader meets it in: absent, which is what an entry that
// declares none prints, filled in, which is what a client that read the old document
// already holds, and null, which the component behind every one of them has always
// refused. `precision` keeps its published spelling: the producer prints it for every
// field, null included, and the document already names null, so that pair already agreed.
func TestTheServedResolvedPolicyAnswersTheServedDocument(t *testing.T) {
	contract := serveDeviceContract(t)
	root := contract.component(t, "Resolved")
	policies := contract.resolvedPolicies(t)
	if len(policies) == 0 {
		t.Fatal("the reference composition serves no resolved policy: the assertions below would say nothing")
	}
	for _, policy := range policies {
		if !resolvedAdmits(t, contract.doc, root, policy, "resolved") {
			t.Error("a served resolved policy does not answer the document the same composition serves")
			break
		}
	}
	member := contract.property(t, "ResolvedReading", "precision")
	if !slices.Contains(resolvedTypeNames(member["type"]), "null") || !slices.Contains(wireStringList(contract.component(t, "ResolvedReading")["required"]), "precision") {
		t.Errorf("precision is published as %v and required by %v; the producer prints it for every reading, null included", member, wireStringList(contract.component(t, "ResolvedReading")["required"]))
	}
	for _, want := range resolvedOptionalMembers {
		resolvedMemberIsOptional(t, contract, want.path, want.component)
	}
	dropped := contract.resolvedTestbed(t)
	delete(dropped, "policyVersion")
	if resolvedAdmits(t, contract.doc, root, dropped, "resolved") {
		t.Error("a resolved policy that drops a member the document demands was admitted")
	}
	added := contract.resolvedTestbed(t)
	added["declaredByNobody"] = ""
	if resolvedAdmits(t, contract.doc, root, added, "resolved") {
		t.Error("a resolved policy carrying a member the document does not name was admitted")
	}
}

// resolvedMemberIsOptional asks one member of the served document and of the served
// body: the document still publishes it as the component it always named and no longer
// demands it, and the body admits it absent, admits it filled in and refuses it as a
// null — the one shape that was never admitted, before the move or after it.
func resolvedMemberIsOptional(t *testing.T, contract deviceContract, path []string, component string) {
	t.Helper()
	member := path[len(path)-1]
	published := contract.property(t, component, member)
	if ref, _ := published["$ref"].(string); !strings.HasPrefix(ref, "#/components/schemas/") {
		t.Errorf("%s.%s is published as %v, not as the component a reader resolves", component, member, published)
	}
	if slices.Contains(wireStringList(contract.component(t, component)["required"]), member) {
		t.Errorf("%s still requires %q, which is what the producer prints for an entry that declares one", component, member)
	}
	shapes := []struct {
		name   string
		value  any
		remove bool
		admits bool
	}{
		{name: "absent", remove: true, admits: true},
		{name: "filled in", value: resolvedExampleOf(t, contract.doc, published), admits: true},
		{name: "printed as null", admits: false},
	}
	for _, shape := range shapes {
		policy := contract.resolvedTestbed(t, path...)
		for _, holder := range resolvedHolders(policy, path) {
			if shape.remove {
				delete(holder, member)
				continue
			}
			holder[member] = shape.value
		}
		if admits := resolvedAdmits(t, contract.doc, contract.component(t, "Resolved"), policy, "resolved"); admits != shape.admits {
			t.Errorf("%s %s: the served document answered %v, want %v", component+"."+member, shape.name, admits, shape.admits)
		}
	}
}

// resolvedHolders walks the path to one member and answers the objects that hold it, so a
// member below a list is asked at every entry that prints one. An empty path asks the
// policy itself, and a body that prints nothing at the path answers none.
func resolvedHolders(policy map[string]any, path []string) []map[string]any {
	holders := []map[string]any{policy}
	for _, step := range path[:max(len(path)-1, 0)] {
		var next []map[string]any
		for _, holder := range holders {
			switch value := holder[step].(type) {
			case map[string]any:
				next = append(next, value)
			case []any:
				for _, item := range value {
					if entry, ok := item.(map[string]any); ok {
						next = append(next, entry)
					}
				}
			}
		}
		holders = next
	}
	return holders
}

// resolvedExampleOf is a value the published member admits: every member its component
// names as required, at the zero of the type that component publishes for it. It is the
// body a client that read the member before the move holds.
func resolvedExampleOf(t *testing.T, doc, member map[string]any) map[string]any {
	t.Helper()
	component := wireResolveRef(doc, member)
	if len(component) == 0 {
		t.Fatalf("%v names no component of the served document", member)
	}
	out := map[string]any{}
	for _, name := range wireStringList(component["required"]) {
		nested := wireMap(wireMap(component["properties"])[name])
		if _, isComponent := nested["$ref"].(string); isComponent {
			out[name] = resolvedExampleOf(t, doc, nested)
			continue
		}
		out[name] = wireZeroValue(nested["type"])
	}
	if len(out) == 0 {
		t.Fatalf("%v demands nothing, so no value of it can be written here", member)
	}
	return out
}

// wireZeroValue is the reading of a published type a validator admits: "" for a string,
// false for a boolean, 0 for a number, [] for a list, {} for an object.
func wireZeroValue(stated any) any {
	for _, name := range resolvedTypeNames(stated) {
		switch name {
		case "string":
			return ""
		case "boolean":
			return false
		case "integer", "number":
			return float64(0)
		case "array":
			return []any{}
		case "object", "null":
			return map[string]any{}
		}
	}
	return map[string]any{}
}

// resolvedTypeNames are the type names a node publishes, of which the document writes one
// name for a single type and a list where it names several.
func resolvedTypeNames(stated any) []string {
	switch typed := stated.(type) {
	case string:
		return []string{typed}
	case []any:
		return wireStringList(typed)
	}
	return nil
}

// resolvedAdmits reports whether value answers the schema node of the served document.
// It reads only the keywords the resolved components are published with — $ref, type
// written as one name or as a set, properties, required, items and additionalProperties —
// and refuses to judge a node that names any other keyword, so the check can only be
// wrong by being incomplete and incompleteness is a failure this case prints by name.
func resolvedAdmits(t *testing.T, doc, schema map[string]any, value any, at string) bool {
	t.Helper()
	for _, keyword := range slices.Sorted(maps.Keys(schema)) {
		switch keyword {
		case "$ref", "additionalProperties", "format", "items", "properties", "required", "type":
		default:
			t.Fatalf("%s publishes %q, which this case cannot read: an answer given here would be a guess", at, keyword)
		}
	}
	if ref, _ := schema["$ref"].(string); ref != "" {
		component := wireResolveRef(doc, map[string]any{"$ref": ref})
		if len(component) == 0 {
			t.Fatalf("%s names %s, which the served document does not carry", at, ref)
		}
		return resolvedAdmits(t, doc, component, value, at+" »"+ref)
	}
	switch typed := value.(type) {
	case nil:
		return resolvedTypeAdmits(t, schema, at, "null")
	case map[string]any:
		return resolvedObjectAdmits(t, doc, schema, typed, at)
	case []any:
		return resolvedListAdmits(t, doc, schema, typed, at)
	case string:
		return resolvedTypeAdmits(t, schema, at, "string")
	case bool:
		return resolvedTypeAdmits(t, schema, at, "boolean")
	case float64:
		return resolvedTypeAdmits(t, schema, at, "integer") || resolvedTypeAdmits(t, schema, at, "number")
	}
	t.Fatalf("%s answers a value this case cannot name: %v", at, value)
	return false
}

func resolvedObjectAdmits(t *testing.T, doc, schema map[string]any, object map[string]any, at string) bool {
	if !resolvedTypeAdmits(t, schema, at, "object") {
		return false
	}
	for _, member := range wireStringList(schema["required"]) {
		if _, held := object[member]; !held {
			return false
		}
	}
	open, openStated := resolvedOpenSlot(schema)
	members := wireMap(schema["properties"])
	for name, value := range object {
		published, declared := members[name]
		switch {
		case declared:
			if !resolvedAdmits(t, doc, wireMap(published), value, at+" ."+name) {
				return false
			}
		case openStated:
			if !resolvedAdmits(t, doc, open, value, at+" {}") {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func resolvedListAdmits(t *testing.T, doc, schema map[string]any, list []any, at string) bool {
	if !resolvedTypeAdmits(t, schema, at, "array") {
		return false
	}
	items, _ := schema["items"].(map[string]any)
	for index, item := range list {
		if !resolvedAdmits(t, doc, items, item, at+" []#"+strconv.Itoa(index)) {
			return false
		}
	}
	return true
}

// resolvedOpenSlot is what a node admits beside the members it names: nothing, anything,
// or one schema.
func resolvedOpenSlot(schema map[string]any) (map[string]any, bool) {
	switch extra := schema["additionalProperties"].(type) {
	case bool:
		if extra {
			return map[string]any{}, true
		}
		return nil, false
	case map[string]any:
		return extra, true
	}
	return nil, false
}

// resolvedTypeAdmits reports whether the node's published `type` names kind. A node that
// names no type constrains nothing by type, which is what a bare reference is.
func resolvedTypeAdmits(t *testing.T, schema map[string]any, at, kind string) bool {
	t.Helper()
	switch stated := schema["type"].(type) {
	case nil:
		return true
	case string, []any:
		return slices.Contains(resolvedTypeNames(stated), kind)
	}
	t.Fatalf("%s publishes a type this case cannot read: %v", at, schema["type"])
	return false
}

// wireSlice is the list a served body holds at name, empty where it holds none.
func wireSlice(node any) []any {
	list, _ := node.([]any)
	return list
}
