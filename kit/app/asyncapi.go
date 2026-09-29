// asyncapi.go renders the composition's event catalogue as an AsyncAPI 3.0.0
// document.
//
// It is a rendering, and only ever a rendering: the module manifest is what
// says which events exist and what they carry (module.Module.Events, kit/module),
// and this walks that list. Nothing here can add an event, and nothing outside
// the Go program has to trust a hand-maintained document that may have drifted
// from the code that publishes. apps/platformkit checks the bytes in as a
// golden file (its own testdata/asyncapi.json), so make check fails when the
// catalogue moved and the committed document did not — the same arrangement
// ui/screens/catalog_test.go keeps for the screen catalog.
//
// The version in info is "1.0.0" and not the build's, because that field
// documents the version of the interface, not of the binary that rendered it:
// this catalogue changes when an event's name or payload schema changes, which
// is a new event name rather than a new document version — a subscriber binds a
// durable to a name, and renaming is the breaking change. The image's own
// revision reaches a footer through the source stamp, which is where a build
// identity belongs.
package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/septagon-oss/platformkit/kit/events/transport"
	"github.com/septagon-oss/platformkit/kit/module"
)

// AsyncAPIVersion is the document version this emits, named because the golden
// file and a reader both need to know which one they are looking at.
const AsyncAPIVersion = "3.0.0"

// asyncapiTitle and asyncapiInterfaceVersion are the two info fields. Neither
// is a build fact: see the package comment.
const (
	asyncapiTitle            = "PlatformKit events"
	asyncapiInterfaceVersion = "1.0.0"
)

// AsyncAPI renders every event every module declared. The error is only about
// encoding: an event with no payload type is rendered with an open `data`
// schema and counted as uncovered rather than dropped, because a document that
// quietly omits an event is worse than one that admits it does not know.
func AsyncAPI(mods []module.Module) ([]byte, error) {
	declared := declaredEvents(withKernel(mods))
	sort.Slice(declared, func(i, j int) bool { return declared[i].Name < declared[j].Name })

	channels := map[string]any{}
	operations := map[string]any{}
	var uncovered []string

	for _, d := range declared {
		if d.Schema() == nil {
			uncovered = append(uncovered, d.Name)
		}
		message := map[string]any{
			"name":        d.Name,
			"title":       d.Name,
			"contentType": "application/json",
			// The payload member IS a Schema Object: AsyncAPI says so, and an
			// integrator, a validator or a code generator reads the body's
			// contract at channels.<name>.messages.<name>.payload. Wrapping the
			// projection in a `schema` member of its own leaves a document that
			// still validates — the AsyncAPI metaschema accepts any keyword — and
			// constrains nothing, which is the failure a validator cannot see.
			"payload": d.Schema().JSONValue(),
			"summary": fmt.Sprintf("%s — %s", moduleName(d.Name), d.Name),
		}
		channels[d.Name] = map[string]any{
			"address": transport.Filter(d.Name),
			"title":   d.Name,
			"summary": "Every tenant's delivery of " + d.Name + ".",
			"description": "Published at " + transport.SubjectPrefix + ".{tenantId}." + d.Name +
				"; the wildcard address is every tenant's delivery of this event, and a durable can be given the exact subject of one tenant. " +
				"The body is a CloudEvents " + transport.SpecVersion + " envelope whose `data` is the payload schema below; `tenantid` is a required extension attribute.",
			"messages": map[string]any{d.Name: message},
		}
		operations[d.Name] = map[string]any{
			"action":  "send",
			"channel": map[string]any{"$ref": "#/channels/" + refEscape(d.Name)},
			"summary": "The " + moduleName(d.Name) + " module publishes " + d.Name + ".",
			"messages": []any{
				map[string]any{"$ref": "#/channels/" + refEscape(d.Name) + "/messages/" + refEscape(d.Name)},
			},
			"tags": []any{map[string]any{"name": moduleName(d.Name)}},
		}
	}

	doc := map[string]any{
		"asyncapi": AsyncAPIVersion,
		"info": map[string]any{
			"title":   asyncapiTitle,
			"version": asyncapiInterfaceVersion,
			"description": "The events this application emits, rendered from the module manifests it was composed from. " +
				"Every event is a CloudEvents " + transport.SpecVersion + " envelope with `tenantid` as a required extension attribute and " +
				"`traceparent`/`tracestate` as the distributed tracing extension; the subject is " +
				transport.SubjectPrefix + ".<tenantId>.<module>.<event>.",
		},
		"defaultContentType": "application/cloudevents+json",
		"channels":           channels,
		"operations":         operations,
	}
	if len(uncovered) > 0 {
		// Named rather than hidden: an integrator who finds an event with an
		// open payload in this document should be able to see that it is known.
		doc["x-uncovered-events"] = uncovered
	}

	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("app: render the AsyncAPI document: %w", err)
	}
	return append(body, '\n'), nil
}

// withKernel adds the kernel's own manifest to a list that may already carry
// it — app.New adds it to the composition, and a caller that built a module
// list by hand has not. declaredEvents de-duplicates by name, so adding it
// twice says the same thing once.
func withKernel(mods []module.Module) []module.Module {
	out := make([]module.Module, 0, len(mods)+1)
	out = append(out, mods...)
	return append(out, kernelModule)
}

// refEscape turns an event name into a JSON pointer segment. AsyncAPI names
// channels by the event name, which contains dots; a pointer segment may not
// contain / or ~, and names contain neither, so this is the escape list and not
// a general encoder.
func refEscape(name string) string { return strings.ReplaceAll(name, "~", "~0") }

// moduleName is the emitter of an event, which is the first segment of its name
// and the manifest that declared it.
func moduleName(name string) string {
	module, _, _ := strings.Cut(name, ".")
	return module
}

// coveredEvents reports how many declared events have a payload schema, as the
// ratio the pillar register asks for. It is exported because the reference
// application prints it at boot, and a number nobody outside a test can read is
// a number nobody keeps at 1.0.
func CoveredEvents(mods []module.Module) (covered, total int) {
	for _, d := range declaredEvents(withKernel(mods)) {
		total++
		if d.Schema() != nil {
			covered++
		}
	}
	return covered, total
}
