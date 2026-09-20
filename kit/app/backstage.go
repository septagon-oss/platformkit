// backstage.go is the composition in Backstage's catalog format: the descriptor
// a service catalog reads to learn which components exist, which of them depend
// on which, and which APIs each provides and consumes.
//
// Like the AsyncAPI document it is a projection of a Description, and it adds no
// fact the composition does not already hold: a Component is a module, a
// dependsOn edge is a subscription, and an API is the event or HTTP surface a
// manifest declares. The catalog is where two teams looking at one installation
// find each other, and this file is what stops that picture from being typed up
// by hand. Nothing reads it at runtime.
//
// YAML is written by struct field order rather than by the alphabet, so a
// descriptor reads in Backstage's own order — type, lifecycle, owner, system —
// and the reference composition's copy under apps/platformkit/testdata stays
// reviewable as a diff.

package app

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// backstageAPIVersion is the catalog schema version the descriptor is written
// under, Backstage's own and not PlatformKit's.
const backstageAPIVersion = "backstage.io/v1alpha1"

// backstageSystem is both the system every component belongs to and the owner
// every component names. There is no group to point at: this repository does not
// know who runs the installation, and an owner Backstage cannot resolve is a
// catalog entry that arrives red.
const backstageSystem = "platformkit"

// The two definition references. Each is a reference and not the document: an
// embedded copy of every module's schema would make this file the largest thing
// in the composition and the one nobody diffs. The AsyncAPI document is the file
// `describe --format asyncapi` writes beside this one; /openapi.json is the one
// the running application serves, which is why it is absolute and the other is
// relative.
const (
	asyncAPIDefinition = "$text: ./asyncapi.json"
	openAPIDefinition  = "$text: /openapi.json"
)

// Backstage returns the composition as a YAML stream of catalog descriptors: the
// system, one Component per module and one API per surface, sorted by name so
// the stream is the same on every machine and a diff shows the module that
// changed rather than the whole file.
//
// The kernel is not in it. Its routes are in the Description so that a reviewer
// can see every operation the application serves, but a Backstage Component is a
// deployable thing, and "kernel" is a label for what no module registered.
func Backstage(d Description) ([]byte, error) {
	components := make([]backstageDoc, 0, len(d.Modules))
	apis := make([]backstageDoc, 0, 2*len(d.Modules))

	mods := make([]ModuleDescription, len(d.Modules))
	copy(mods, d.Modules)
	sort.Slice(mods, func(i, j int) bool { return mods[i].Name < mods[j].Name })

	for _, m := range mods {
		if m.Name == kernelModule {
			continue
		}
		spec := backstageComponentSpec{
			Type: "service", Lifecycle: "production", Owner: backstageSystem, System: backstageSystem,
			DependsOn:    subscribedAs(m, "component:%s"),
			ConsumesApis: subscribedAs(m, "api:%s-events"),
		}
		if len(m.Events) > 0 {
			spec.ProvidesApis = append(spec.ProvidesApis, "api:"+m.Name+"-events")
		}
		if len(m.Routes) > 0 {
			spec.ProvidesApis = append(spec.ProvidesApis, "api:"+m.Name+"-http")
		}
		components = append(components, backstageDoc{
			APIVersion: backstageAPIVersion, Kind: "Component",
			Metadata: backstageMeta{Name: m.Name}, Spec: spec,
		})
		// Each module's pair travels together, which is name order already:
		// <module>-events sorts before <module>-http.
		if len(m.Events) > 0 {
			apis = append(apis, backstageAPI(m.Name+"-events", "asyncapi", asyncAPIDefinition))
		}
		if len(m.Routes) > 0 {
			apis = append(apis, backstageAPI(m.Name+"-http", "openapi", openAPIDefinition))
		}
	}

	var out bytes.Buffer
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	docs := append([]backstageDoc{{
		APIVersion: backstageAPIVersion, Kind: "System",
		Metadata: backstageMeta{Name: backstageSystem}, Spec: struct{}{},
	}}, components...)
	docs = append(docs, apis...)
	for _, doc := range docs {
		if err := enc.Encode(doc); err != nil {
			return nil, fmt.Errorf("app: describe: the %s named %q did not encode: %w", doc.Kind, doc.Metadata.Name, err)
		}
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("app: describe: the catalog stream did not close: %w", err)
	}
	return out.Bytes(), nil
}

// subscribedAs is every module this one subscribes to, named by the prefix of the
// event's name and formatted for the field it is heading to. A subscription is
// the composition's only dependency edge, so dependsOn and consumesApis are the
// same list twice in two vocabularies rather than two things to keep in step.
//
// A module that listens to an event of its own appears here too: it does consume
// its own events, and leaving it out would say the edge does not exist.
func subscribedAs(m ModuleDescription, format string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range m.Subscriptions {
		owner, _, _ := strings.Cut(s, ".")
		if seen[owner] {
			continue
		}
		seen[owner] = true
		out = append(out, fmt.Sprintf(format, owner))
	}
	sort.Strings(out)
	return out
}

func backstageAPI(name, kind, definition string) backstageDoc {
	return backstageDoc{
		APIVersion: backstageAPIVersion, Kind: "API",
		Metadata: backstageMeta{Name: name},
		Spec:     backstageAPISpec{Type: kind, Definition: definition},
	}
}

type backstageDoc struct {
	APIVersion string        `yaml:"apiVersion"`
	Kind       string        `yaml:"kind"`
	Metadata   backstageMeta `yaml:"metadata"`
	// Spec is whatever the kind takes. yaml.v3 writes a value's struct field
	// order, which is the point of the types below.
	Spec any `yaml:"spec"`
}

type backstageMeta struct {
	Name string `yaml:"name"`
}

type backstageComponentSpec struct {
	Type         string   `yaml:"type"`
	Lifecycle    string   `yaml:"lifecycle"`
	Owner        string   `yaml:"owner"`
	System       string   `yaml:"system"`
	DependsOn    []string `yaml:"dependsOn,omitempty"`
	ProvidesApis []string `yaml:"providesApis,omitempty"`
	ConsumesApis []string `yaml:"consumesApis,omitempty"`
}

type backstageAPISpec struct {
	Type       string `yaml:"type"`
	Definition string `yaml:"definition"`
}
