package components

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"

	g "maragu.dev/gomponents"
)

// ExampleInfo identifies a particular constructor invocation, not a second
// component registry. IDs are supplied by the owner of the example collection.
// ComponentID names one Props/slot interface independently of example grouping;
// helpers with different interfaces need distinct component identities.
type ExampleInfo struct {
	ID          string `json:"id"`
	ComponentID string `json:"componentId"`
	Group       string `json:"group"`
	Name        string `json:"name"`
}

// Example captures typed inputs while keeping the existing gallery renderer.
// Props and slot containers are copied. Nodes and callbacks remain opaque,
// trusted Go capabilities: callers must not mutate state captured by them.
type Example struct {
	ExampleInfo
	Node   g.Node
	props  reflect.Value
	slots  reflect.Value
	render func(reflect.Value, reflect.Value) g.Node
	reason string
}

// ExampleDescription is a flat export of one actual constructor invocation.
// PropsEditable and slot support describe Go APIs, not native/editor readiness.
type ExampleDescription struct {
	ExampleInfo
	PropsEditable bool              `json:"propsEditable"`
	Reason        string            `json:"reason,omitempty"`
	Props         json.RawMessage   `json:"props"`
	Schema        json.RawMessage   `json:"schema"`
	Slots         []SlotDescription `json:"slots"`
	HTML          string            `json:"html"`
}

// SlotDescription advertises the actual field type. Supported replacements
// accept trusted Go nodes only; callbacks and compound slot data are read-only.
type SlotDescription struct {
	Name        string `json:"name"`
	GoType      string `json:"goType"`
	Supported   bool   `json:"supported"`
	Multiple    bool   `json:"multiple"`
	TrustedOnly bool   `json:"trustedOnly"`
}

var exampleNodeType = reflect.TypeFor[g.Node]()
var exampleNodesType = reflect.TypeFor[[]g.Node]()

// ExampleOf captures Props and delegates all rendering to render.
func ExampleOf[P any](info ExampleInfo, props P, render func(P) g.Node) Example {
	return ExampleWithSlots(info, props, struct{}{}, func(p P, _ struct{}) g.Node { return render(p) })
}

// ExampleWithSlots captures the actual slot struct beside its Props.
func ExampleWithSlots[P, S any](info ExampleInfo, props P, slots S, render func(P, S) g.Node) Example {
	e := Example{ExampleInfo: info, props: reflect.ValueOf(&props).Elem(), slots: reflect.ValueOf(&slots).Elem()}
	e.render = func(p, s reflect.Value) g.Node { return render(p.Interface().(P), s.Interface().(S)) }
	return e.capture()
}

// ExampleWithChildren names an existing variadic constructor's node slice
// "children". It does not introduce a portable child graph or renderer.
func ExampleWithChildren[P any](info ExampleInfo, props P, children []g.Node, render func(P, ...g.Node) g.Node) Example {
	type childrenSlot struct {
		Children []g.Node `json:"children"`
	}
	return ExampleWithSlots(info, props, childrenSlot{children}, func(p P, s childrenSlot) g.Node { return render(p, s.Children...) })
}

// ExamplePreview explicitly records a helper without an editable Props contract.
func ExamplePreview(info ExampleInfo, node g.Node, reason string) Example {
	if reason == "" {
		reason = "helper has no typed Props contract"
	}
	return Example{ExampleInfo: info, Node: node, reason: reason}
}

func (e Example) capture() Example {
	e.props, e.slots = copyExampleValue(e.props), copyExampleValue(e.slots)
	e.Node = g.NodeFunc(func(w io.Writer) error {
		node := e.render(copyExampleValue(e.props), copyExampleValue(e.slots))
		if node == nil {
			return fmt.Errorf("example %q rendered a nil node", e.ID)
		}
		return node.Render(w)
	})
	return e
}

// Describe projects only portable data and uses the captured constructor for HTML.
func (e Example) Describe() (ExampleDescription, error) {
	d := ExampleDescription{ExampleInfo: e.ExampleInfo, Reason: e.reason, Slots: []SlotDescription{}}
	if e.render != nil {
		if e.props.Kind() != reflect.Struct || e.slots.Kind() != reflect.Struct {
			return d, fmt.Errorf("example %q requires struct Props and slots", e.ID)
		}
		schema, err := exampleSchema(e.props.Type())
		if err != nil {
			return d, err
		}
		schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
		d.Schema, err = json.Marshal(schema)
		if err != nil {
			return d, err
		}
		value, err := portableExampleValue(e.props, map[exampleCopyKey]bool{})
		if err != nil {
			return d, err
		}
		d.Props, err = json.Marshal(value)
		if err != nil {
			return d, err
		}
		fields, err := exampleFields(e.slots.Type())
		if err != nil {
			return d, err
		}
		for _, field := range fields {
			t := field.typ
			d.Slots = append(d.Slots, SlotDescription{field.name, t.String(), t == exampleNodeType || t == exampleNodesType, t == exampleNodesType, true})
		}
		d.PropsEditable = true
	}
	if e.Node == nil {
		return d, fmt.Errorf("example %q has no node", e.ID)
	}
	var html strings.Builder
	if err := e.Node.Render(&html); err != nil {
		return d, err
	}
	d.HTML = html.String()
	return d, nil
}

// WithProps applies a strict, exact-case top-level JSON patch. Each supplied
// nested value replaces that field in full; unspecified trusted transport
// fields are retained. This is typed data editing, not HTML sanitization.
func (e Example) WithProps(patch json.RawMessage) (Example, error) {
	if e.render == nil || e.props.Kind() != reflect.Struct {
		return Example{}, fmt.Errorf("example %q has no editable Props contract", e.ID)
	}
	fields, err := exampleFields(e.props.Type())
	if err != nil {
		return Example{}, err
	}
	values, err := exampleObject(patch)
	if err != nil {
		return Example{}, err
	}
	props := copyExampleValue(e.props)
	for name, raw := range values {
		index := slices.IndexFunc(fields, func(f exampleField) bool { return f.name == name })
		if index < 0 {
			return Example{}, fmt.Errorf("unknown or internal property %q", name)
		}
		field := fields[index]
		value, err := decodeExampleValue(raw, field.typ)
		if err != nil {
			return Example{}, fmt.Errorf("property %q: %w", name, err)
		}
		exampleFieldValue(props, field.index).Set(value)
	}
	e.props = props
	return e.capture(), nil
}

// WithSlot replaces one actual Node or []Node field without changing the
// original example. No JSON-to-node conversion, callback editing, or traversal
// of compound slots is supported. Passing no nodes clears a supported slot.
func (e Example) WithSlot(name string, nodes ...g.Node) (Example, error) {
	if e.render == nil || e.slots.Kind() != reflect.Struct {
		return Example{}, fmt.Errorf("example %q has no replaceable slots", e.ID)
	}
	fields, err := exampleFields(e.slots.Type())
	if err != nil {
		return Example{}, err
	}
	for _, field := range fields {
		if field.name != name {
			continue
		}
		if field.typ != exampleNodeType && field.typ != exampleNodesType {
			return Example{}, fmt.Errorf("slot %q (%s) is not a Node or []Node field", name, field.typ)
		}
		if field.typ == exampleNodeType && len(nodes) > 1 {
			return Example{}, fmt.Errorf("slot %q accepts at most one node", name)
		}
		e.slots = copyExampleValue(e.slots)
		value := reflect.Zero(field.typ)
		if field.typ == exampleNodesType {
			value = reflect.ValueOf(slices.Clone(nodes))
		} else if len(nodes) == 1 && nodes[0] != nil {
			value = reflect.ValueOf(nodes[0])
		}
		exampleFieldValue(e.slots, field.index).Set(value)
		return e.capture(), nil
	}
	return Example{}, fmt.Errorf("unknown slot %q", name)
}

type exampleCopyKey struct {
	typ reflect.Type
	ptr uintptr
	len int
}

func exampleValueKey(v reflect.Value) exampleCopyKey {
	k := exampleCopyKey{typ: v.Type()}
	if v.Kind() == reflect.Pointer || v.Kind() == reflect.Map || v.Kind() == reflect.Slice {
		k.ptr = uintptr(v.UnsafePointer())
	}
	if v.Kind() == reflect.Slice {
		k.len = v.Len()
	}
	return k
}

func copyExampleValue(value reflect.Value) reflect.Value {
	seen := map[exampleCopyKey]reflect.Value{}
	var clone func(reflect.Value) reflect.Value
	clone = func(v reflect.Value) reflect.Value {
		if !v.IsValid() || v.Type().Implements(exampleNodeType) {
			return v
		}
		key := exampleValueKey(v)
		if prior, ok := seen[key]; key.ptr != 0 && ok {
			return prior
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				if v.Kind() == reflect.Pointer {
					out.Set(reflect.New(v.Type().Elem()))
					seen[key] = out
					out.Elem().Set(clone(v.Elem()))
				} else {
					out.Set(clone(v.Elem()))
				}
			}
		case reflect.Map:
			if !v.IsNil() {
				out.Set(reflect.MakeMapWithSize(v.Type(), v.Len()))
				seen[key] = out
				for iter := v.MapRange(); iter.Next(); {
					out.SetMapIndex(iter.Key(), clone(iter.Value()))
				}
			}
		case reflect.Slice, reflect.Array:
			if v.Kind() == reflect.Slice {
				if v.IsNil() {
					return out
				}
				out.Set(reflect.MakeSlice(v.Type(), v.Len(), v.Len()))
				seen[key] = out
			}
			for i := range v.Len() {
				out.Index(i).Set(clone(v.Index(i)))
			}
		case reflect.Struct:
			for i := range v.NumField() {
				if out.Field(i).CanSet() && v.Type().Field(i).IsExported() {
					out.Field(i).Set(clone(v.Field(i)))
				}
			}
		}
		return out
	}
	return clone(value)
}

func exampleObject(raw []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected JSON object")
	}
	object := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name := token.(string)
		if _, exists := object[name]; exists {
			return nil, fmt.Errorf("duplicate property %q", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		object[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after JSON object")
	}
	return object, nil
}

func decodeExampleValue(raw []byte, typ reflect.Type) (reflect.Value, error) {
	out := reflect.New(typ).Elem()
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		switch typ.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
			return out, nil
		default:
			return out, fmt.Errorf("null is not a %s", typ)
		}
	}
	if typ.Kind() == reflect.Pointer {
		value, err := decodeExampleValue(raw, typ.Elem())
		if err == nil {
			out.Set(reflect.New(typ.Elem()))
			out.Elem().Set(value)
		}
		return out, err
	}
	if typ.Kind() == reflect.Struct {
		fields, err := exampleFields(typ)
		if err != nil {
			return out, err
		}
		values, err := exampleObject(raw)
		if err != nil {
			return out, err
		}
		for name, data := range values {
			i := slices.IndexFunc(fields, func(f exampleField) bool { return f.name == name })
			if i < 0 {
				return out, fmt.Errorf("unknown or internal property %q", name)
			}
			value, err := decodeExampleValue(data, fields[i].typ)
			if err != nil {
				return out, err
			}
			exampleFieldValue(out, fields[i].index).Set(value)
		}
		return out, nil
	}
	if typ.Kind() == reflect.Map && typ.Key().Kind() == reflect.String {
		values, err := exampleObject(raw)
		if err != nil {
			return out, err
		}
		out.Set(reflect.MakeMapWithSize(typ, len(values)))
		for name, data := range values {
			value, err := decodeExampleValue(data, typ.Elem())
			if err != nil {
				return out, err
			}
			out.SetMapIndex(reflect.ValueOf(name).Convert(typ.Key()), value)
		}
		return out, nil
	}
	byteSlice := typ.Kind() == reflect.Slice && typ.Elem().Kind() == reflect.Uint8
	if (typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array) && !byteSlice {
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return out, err
		}
		if typ.Kind() == reflect.Array && len(values) != typ.Len() {
			return out, fmt.Errorf("expected array of length %d", typ.Len())
		}
		if typ.Kind() == reflect.Slice {
			out.Set(reflect.MakeSlice(typ, len(values), len(values)))
		}
		for i, data := range values {
			value, err := decodeExampleValue(data, typ.Elem())
			if err != nil {
				return out, err
			}
			out.Index(i).Set(value)
		}
		return out, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if typ == reflect.TypeFor[json.Number]() {
		var value any
		if err := decoder.Decode(&value); err != nil {
			return out, err
		}
		number, ok := value.(json.Number)
		if !ok {
			return out, fmt.Errorf("expected JSON number")
		}
		out.SetString(number.String())
		return out, nil
	}
	return out, decoder.Decode(out.Addr().Interface())
}
