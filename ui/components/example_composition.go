package components

import (
	"fmt"
	"io"
	"strings"

	g "maragu.dev/gomponents"
)

// ChildOccurrence retains a source-local invocation identity, not a native node
// ID. Enclosing identities distinguish placements of the same nested definition.
// Slot names a declared input, not proof that rendering fetched that field; it
// is empty when named ownership is unresolved. A nil Span means unobserved,
// not absent: opaque Go nodes may buffer, omit or duplicate their children's HTML.
type ChildOccurrence struct {
	Description ExampleDescription `json:"description"`
	Slot        string             `json:"slot,omitempty"`
	Span        *HTMLSpan          `json:"span,omitempty"`
}

// HTMLSpan addresses bytes in the immediate parent's Description.HTML. It is
// evidence from one synchronous render, never a durable identity or DOM address.
// Equal endpoints describe an observed invocation that emitted no bytes.
type HTMLSpan struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type exampleNode struct{ example Example }

func (n *exampleNode) Render(w io.Writer) error {
	if recorder, ok := w.(*exampleRecorder); ok {
		return recorder.observe(n)
	}
	return n.renderBody(w)
}

func (n *exampleNode) renderBody(w io.Writer) error {
	e := n.example
	node := e.render(copyExampleValue(e.props), copyExampleValue(e.slots))
	if node == nil {
		return fmt.Errorf("example %q rendered a nil node", e.ID)
	}
	return node.Render(w)
}

func (n *exampleNode) String() string {
	var out strings.Builder
	_ = n.Render(&out)
	return out.String()
}

func (e Example) validateCapture() error {
	if e.Node == nil {
		return fmt.Errorf("example %q has no node", e.ID)
	}
	if e.render != nil {
		node, ok := e.Node.(*exampleNode)
		if !ok || node == nil || node != e.bound || node.example.ID != e.ID || node.example.ComponentID != e.ComponentID {
			return fmt.Errorf("example %q no longer matches its captured node or identity", e.ID)
		}
	}
	return nil
}

// Describe observes one synchronous rendering through the existing constructors.
// Children describe bound invocations, not all HTML nodes. OpaqueSlots identifies
// supported slots containing unbound, non-nil Go nodes; neither their complete
// contents nor their rendering correspondence can be inferred from this record.
func (e Example) Describe() (ExampleDescription, error) {
	if err := e.validateCapture(); err != nil {
		return ExampleDescription{}, err
	}
	root, err := newExampleRecord(e, e.bound, map[*exampleNode]bool{})
	if err != nil {
		return ExampleDescription{}, err
	}
	recorder := &exampleRecorder{current: root, active: map[*exampleNode]bool{}}
	if e.bound == nil {
		root.observed = true
		err = e.Node.Render(recorder)
		root.end = recorder.output.Len()
	} else {
		err = recorder.render(root)
	}
	recorder.closed = true
	if err == nil {
		err = recorder.failure
	}
	if err != nil {
		return ExampleDescription{}, err
	}
	return root.describe(recorder.output.String()), nil
}

type exampleRecord struct {
	node        *exampleNode
	description ExampleDescription
	slot        string
	children    []*exampleRecord
	byID        map[string]*exampleRecord
	observed    bool
	start, end  int
}

func newExampleRecord(e Example, node *exampleNode, active map[*exampleNode]bool) (*exampleRecord, error) {
	if strings.TrimSpace(e.ID) == "" || strings.TrimSpace(e.ComponentID) == "" {
		return nil, fmt.Errorf("example %q has no stable identity", e.ID)
	}
	if node != nil {
		if active[node] {
			return nil, fmt.Errorf("cyclic example capture %q", e.ID)
		}
		active[node] = true
		defer delete(active, node)
	}
	description, err := e.describeInputs()
	if err != nil {
		return nil, err
	}
	record := &exampleRecord{node: node, description: description, byID: map[string]*exampleRecord{}}
	if e.render == nil {
		return record, nil
	}
	fields, err := exampleFields(e.slots.Type())
	if err != nil {
		return nil, err
	}
	slots := copyExampleValue(e.slots)
	for _, field := range fields {
		if field.typ != exampleNodeType && field.typ != exampleNodesType {
			continue
		}
		value := exampleFieldValue(slots, field.index)
		var nodes []g.Node
		if field.typ == exampleNodesType {
			nodes = value.Interface().([]g.Node)
		} else if !value.IsNil() {
			nodes = []g.Node{value.Interface().(g.Node)}
		}
		opaque := false
		for _, supplied := range nodes {
			if supplied == nil {
				continue
			}
			bound, ok := supplied.(*exampleNode)
			if !ok {
				opaque = true
				continue
			}
			if bound == nil {
				return nil, fmt.Errorf("slot %q contains a nil capture", field.name)
			}
			child, err := newExampleRecord(bound.example, bound, active)
			if err != nil {
				return nil, err
			}
			child.slot = field.name
			if err := record.add(child); err != nil {
				return nil, err
			}
		}
		if opaque {
			record.description.OpaqueSlots = append(record.description.OpaqueSlots, field.name)
		}
	}
	return record, nil
}

func (r *exampleRecord) add(child *exampleRecord) error {
	if _, exists := r.byID[child.description.ID]; exists {
		return fmt.Errorf("example %q has duplicate child identity %q", r.description.ID, child.description.ID)
	}
	r.byID[child.description.ID] = child
	r.children = append(r.children, child)
	return nil
}

func (r *exampleRecord) describe(html string) ExampleDescription {
	description := r.description
	if r.observed {
		description.HTML = html[r.start:r.end]
	}
	for _, child := range r.children {
		occurrence := ChildOccurrence{Description: child.describe(html), Slot: child.slot}
		if child.observed {
			occurrence.Span = &HTMLSpan{Start: child.start - r.start, End: child.end - r.start}
		}
		description.Children = append(description.Children, occurrence)
	}
	return description
}

// The recorder owns only this rendering. It exposes no reset/truncate operation
// and does not follow a writer hidden inside an arbitrary Go node or callback.
type exampleRecorder struct {
	output  strings.Builder
	current *exampleRecord
	active  map[*exampleNode]bool
	closed  bool
	failure error
}

func (r *exampleRecorder) Write(data []byte) (int, error) {
	if r.closed {
		return 0, fmt.Errorf("example render is closed")
	}
	return r.output.Write(data)
}

func (r *exampleRecorder) WriteString(data string) (int, error) {
	if r.closed {
		return 0, fmt.Errorf("example render is closed")
	}
	return r.output.WriteString(data)
}

func (r *exampleRecorder) observe(node *exampleNode) (err error) {
	defer func() {
		if err != nil && r.failure == nil {
			r.failure = err
		}
	}()
	if r.closed || r.current == nil || node == nil {
		return fmt.Errorf("example rendering has no active owner")
	}
	if r.active[node] {
		return fmt.Errorf("cyclic example rendering %q", node.example.ID)
	}
	child, exists := r.current.byID[node.example.ID]
	if exists && child.node != node {
		return fmt.Errorf("example %q has conflicting child capture %q", r.current.description.ID, node.example.ID)
	}
	if !exists {
		var err error
		child, err = newExampleRecord(node.example, node, map[*exampleNode]bool{})
		if err != nil {
			return err
		}
		if err := r.current.add(child); err != nil {
			return err
		}
	}
	return r.render(child)
}

func (r *exampleRecorder) render(record *exampleRecord) error {
	if record.observed {
		return fmt.Errorf("example %q rendered the same occurrence more than once", record.description.ID)
	}
	record.observed, record.start = true, r.output.Len()
	previous := r.current
	r.current, r.active[record.node] = record, true
	completed := false
	defer func() {
		record.end = r.output.Len()
		r.current = previous
		delete(r.active, record.node)
		if !completed && r.failure == nil {
			r.failure = fmt.Errorf("example %q rendering did not complete", record.description.ID)
		}
	}()
	err := record.node.renderBody(r)
	completed = true
	return err
}
