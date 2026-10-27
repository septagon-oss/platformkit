package screen

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Operation is a recorded route identity and its canonical authorization guard.
type Operation struct {
	ID      string `json:"id"`
	Surface string `json:"surface"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Guard   string `json:"guard"`
}

// Response binds a mounted route to a possible input, result or redirect view.
type Response struct {
	View ViewRef
	Kind string
}

// Mount is an observation from the real router. Exceptions are limited to
// redirect, protocol, export and preview responses and need a reason.
type Mount struct {
	Targets   []Target
	Operation Operation
	Owner     string
	Document  bool
	Responses []Response
	Exception string
	Reason    string
}

// RouteData records one response variant of a view in the artifact.
type RouteData struct {
	Surface     string `json:"surface"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	OperationID string `json:"operationId"`
	Response    string `json:"response"`
	Guard       string `json:"guard"`
}

// AnchorData is the serialized descriptor and its semantic identity digest.
type AnchorData struct {
	Name         string        `json:"name"`
	Interactions []Interaction `json:"interactions"`
	Binding      BindingData   `json:"binding"`
	Control      string        `json:"control"`
	IdentityHash string        `json:"identityHash"`
}

// ViewData is the serialized view, including all mounted response variants.
type ViewData struct {
	ID           ViewRef      `json:"id"`
	Owner        string       `json:"owner"`
	Kind         string       `json:"kind"`
	Cardinality  Cardinality  `json:"cardinality"`
	Hosts        []ViewRef    `json:"hosts"`
	Routes       []RouteData  `json:"routes"`
	Anchors      []AnchorData `json:"anchors"`
	IdentityHash string       `json:"identityHash"`
}

// SnapshotData is the versioned wire format, independent of descriptor storage.
type SnapshotData struct {
	SchemaVersion int         `json:"schemaVersion"`
	App           string      `json:"app"`
	PolicyVersion int         `json:"policyVersion"`
	Operations    []Operation `json:"operations"`
	Views         []ViewData  `json:"views"`
	Hash          string      `json:"hash"`
}

// Inventory is a validated immutable composition. It contains no request data.
type Inventory struct {
	data  SnapshotData
	views []View
}

// Publish validates declarations against actual mounts, then freezes a sorted,
// hashed inventory. Failure returns no partial inventory and performs no effects.
func Publish(app string, policy int, declarations []View, mounts []Mount) (Inventory, error) {
	if _, err := Segment(app); err != nil || policy != 1 {
		return Inventory{}, fmt.Errorf("screen: invalid application or unsupported policy version %d", policy)
	}
	data := SnapshotData{SchemaVersion: 1, App: app, PolicyVersion: policy, Operations: []Operation{}, Views: []ViewData{}}
	indices := map[ViewRef]int{}
	for _, view := range declarations {
		if _, err := ParseView(string(view.Ref())); err != nil {
			return Inventory{}, err
		}
		if _, exists := indices[view.Ref()]; exists {
			return Inventory{}, fmt.Errorf("screen: duplicate view %s", view.Ref())
		}
		indices[view.Ref()] = len(data.Views)
		data.Views = append(data.Views, viewData(view))
	}
	ops, err := observeMounts(mounts, indices, &data)
	if err != nil {
		return Inventory{}, err
	}
	for _, m := range mounts {
		for _, target := range m.Targets {
			op, ok := ops[target.OperationID]
			if !ok || op.Method != target.Method || op.Path != target.Path {
				return Inventory{}, fmt.Errorf("screen: target %s does not serve %s %s", target.OperationID, target.Method, target.Path)
			}
		}
	}
	used := map[string]bool{}
	for _, view := range data.Views {
		if err := validateTargets(view, indices, ops, used); err != nil {
			return Inventory{}, err
		}
	}
	for id := range used {
		data.Operations = append(data.Operations, ops[id])
	}
	canonicalize(&data)
	if err := seal(&data); err != nil {
		return Inventory{}, err
	}
	return Inventory{data: data, views: slices.Clone(declarations)}, nil
}

func viewData(v View) ViewData {
	out := ViewData{ID: v.Ref(), Owner: v.Ref().Owner(), Kind: v.Ref().Kind(), Cardinality: v.Cardinality(), Hosts: v.Hosts(), Routes: []RouteData{}, Anchors: []AnchorData{}}
	for _, a := range v.Anchors() {
		out.Anchors = append(out.Anchors, AnchorData{Name: a.Name(), Interactions: a.Interactions(), Binding: a.Binding(), Control: a.Control()})
	}
	return out
}

func observeMounts(mounts []Mount, indices map[ViewRef]int, data *SnapshotData) (map[string]Operation, error) {
	ops := map[string]Operation{}
	for _, m := range mounts {
		op := m.Operation
		if err := validateOperation(op); err != nil {
			return nil, err
		}
		if _, exists := ops[op.ID]; exists {
			return nil, fmt.Errorf("screen: duplicate operation %s", op.ID)
		}
		ops[op.ID] = op
		if m.Document && len(m.Responses) == 0 && (!slices.Contains([]string{"redirect", "protocol", "export", "preview"}, m.Exception) || m.Reason == "") {
			return nil, fmt.Errorf("screen: document operation %s has no view or response classification", op.ID)
		}
		if len(m.Responses) != 0 && (m.Exception != "" || !m.Document) {
			return nil, fmt.Errorf("screen: operation %s has conflicting response declarations", op.ID)
		}
		seen := map[Response]bool{}
		for _, r := range m.Responses {
			i, found := indices[r.View]
			if !found || m.Owner != r.View.Owner() || !slices.Contains([]string{"input", "result", "redirect"}, r.Kind) || seen[r] {
				return nil, fmt.Errorf("screen: operation %s has invalid view response %s (%s)", op.ID, r.View, r.Kind)
			}
			if strings.Split(string(r.View), "/")[0] != op.Surface {
				return nil, fmt.Errorf("screen: view %s is mounted on %s", r.View, op.Surface)
			}
			seen[r] = true
			data.Views[i].Routes = append(data.Views[i].Routes, RouteData{op.Surface, op.Method, op.Path, op.ID, r.Kind, op.Guard})
		}
	}
	return ops, nil
}

func validateOperation(op Operation) error {
	if _, err := Segment(op.ID); err != nil || !slices.Contains([]string{"app", "public", "ops"}, op.Surface) ||
		!slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}, op.Method) ||
		!strings.HasPrefix(op.Path, "/") || strings.ContainsAny(op.Path, "?#\r\n") || op.Guard == "" {
		return fmt.Errorf("screen: invalid operation %q", op.ID)
	}
	return nil
}

func validateTargets(v ViewData, views map[ViewRef]int, operations map[string]Operation, used map[string]bool) error {
	if len(v.Routes) == 0 {
		return fmt.Errorf("screen: view %s has no mounted response", v.ID)
	}
	if v.Kind == "command" && len(v.Hosts) == 0 {
		return fmt.Errorf("screen: command %s has no mounted host", v.ID)
	}
	for _, host := range v.Hosts {
		if _, ok := views[host]; !ok {
			return fmt.Errorf("screen: %s has missing host %s", v.ID, host)
		}
	}
	for _, r := range v.Routes {
		used[r.OperationID] = true
	}
	for _, a := range v.Anchors {
		b := a.Binding
		if b.Kind == "navigation" {
			if _, ok := views[ViewRef(b.Target)]; !ok {
				return fmt.Errorf("screen: %s#%s has missing destination %s", v.ID, a.Name, b.Target)
			}
		} else if b.Kind != "display" {
			if _, ok := operations[b.Target]; !ok {
				return fmt.Errorf("screen: %s#%s has missing operation %s", v.ID, a.Name, b.Target)
			}
			used[b.Target] = true
		}
	}
	return nil
}

func canonicalize(data *SnapshotData) {
	slices.SortFunc(data.Operations, func(a, b Operation) int { return cmp.Compare(a.ID, b.ID) })
	slices.SortFunc(data.Views, func(a, b ViewData) int { return cmp.Compare(a.ID, b.ID) })
	for i := range data.Views {
		v := &data.Views[i]
		slices.Sort(v.Hosts)
		slices.SortFunc(v.Routes, func(a, b RouteData) int {
			return cmp.Or(cmp.Compare(a.Surface, b.Surface), cmp.Compare(a.Method, b.Method), cmp.Compare(a.Path, b.Path), cmp.Compare(a.OperationID, b.OperationID), cmp.Compare(a.Response, b.Response))
		})
		slices.SortFunc(v.Anchors, func(a, b AnchorData) int { return cmp.Compare(a.Name, b.Name) })
		for j := range v.Anchors {
			a := &v.Anchors[j]
			slices.Sort(a.Interactions)
			slices.SortFunc(a.Binding.Fixed, func(a, b FixedInput) int {
				return cmp.Or(cmp.Compare(a.Location, b.Location), cmp.Compare(pathName(a.FieldPath), pathName(b.FieldPath)))
			})
		}
	}
}

// View resolves an exact reference. Invalid and absent references are errors.
func (i Inventory) View(ref ViewRef) (View, error) {
	if _, err := ParseView(string(ref)); err != nil {
		return View{}, err
	}
	for _, v := range i.views {
		if v.Ref() == ref {
			return v, nil
		}
	}
	return View{}, fmt.Errorf("screen: missing view %s", ref)
}

// Anchor resolves a unique control; it never chooses a repeated instance.
func (i Inventory) Anchor(ref AnchorRef) (Anchor, error) {
	if _, err := ParseAnchor(string(ref)); err != nil {
		return Anchor{}, err
	}
	view, _, _ := strings.Cut(string(ref), "#")
	v, err := i.View(ViewRef(view))
	if err != nil {
		return Anchor{}, err
	}
	return v.Anchor(ref)
}
