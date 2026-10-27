package screen

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"slices"
)

func canonical(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var object any
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	if err := d.Decode(&object); err != nil {
		return nil, err
	}
	return json.Marshal(object)
}

func digest(value any) string {
	b, err := canonical(value)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(b))
}

func operationIdentity(o Operation) map[string]any {
	return map[string]any{"id": o.ID, "surface": o.Surface, "method": o.Method, "path": o.Path}
}

func seal(data *SnapshotData) error {
	operations := map[string]any{}
	views := map[string]any{}
	for _, o := range data.Operations {
		operations[o.ID] = operationIdentity(o)
	}
	for i := range data.Views {
		v := &data.Views[i]
		routes := []map[string]any{}
		for _, r := range v.Routes {
			routes = append(routes, map[string]any{"surface": r.Surface, "method": r.Method, "path": r.Path, "operationId": r.OperationID, "response": r.Response})
		}
		v.IdentityHash = digest(map[string]any{"id": v.ID, "owner": v.Owner, "kind": v.Kind, "cardinality": v.Cardinality, "hosts": v.Hosts, "routes": routes})
		views[string(v.ID)] = v.IdentityHash
	}
	for i := range data.Views {
		v := &data.Views[i]
		for j := range v.Anchors {
			a := &v.Anchors[j]
			target := operations[a.Binding.Target]
			if a.Binding.Kind == "navigation" {
				target = views[a.Binding.Target]
			}
			if a.Binding.Kind == "display" {
				target = a.Binding.Target
			}
			if target == nil {
				return fmt.Errorf("screen: missing binding target %s", a.Binding.Target)
			}
			a.IdentityHash = digest(map[string]any{"view": v.IdentityHash, "name": a.Name, "binding": a.Binding, "targetIdentity": target})
		}
	}
	data.Hash = digest(map[string]any{"schemaVersion": data.SchemaVersion, "app": data.App, "policyVersion": data.PolicyVersion, "operations": data.Operations, "views": data.Views})
	return nil
}

// Bytes returns the sole persisted representation: sorted JSON, two-space
// indentation and a final newline. An uninitialized inventory is refused.
func (i Inventory) Bytes() ([]byte, error) {
	if i.data.Hash == "" {
		return nil, fmt.Errorf("screen: uninitialized inventory")
	}
	compact, err := canonical(i.data)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Indent(&out, compact, "", "  "); err != nil {
		return nil, err
	}
	out.WriteByte('\n')
	return out.Bytes(), nil
}

// Snapshot returns a detached wire value for exporters and generators.
func (i Inventory) Snapshot() SnapshotData {
	b, _ := json.Marshal(i.data)
	var data SnapshotData
	_ = json.Unmarshal(b, &data)
	return data
}

// Compatible refuses a retained reference whose semantic binding changed.
// Removed references are left to generated-reference and consumer checks.
func (i Inventory) Compatible(previous Inventory) error {
	if i.data.SchemaVersion != previous.data.SchemaVersion || i.data.App != previous.data.App {
		return fmt.Errorf("screen: incompatible application or schema baseline")
	}
	old := map[string]string{}
	for _, v := range previous.data.Views {
		old[string(v.ID)] = v.IdentityHash
		for _, a := range v.Anchors {
			old[string(v.ID)+"#"+a.Name] = a.IdentityHash
		}
	}
	for _, v := range i.data.Views {
		current := map[string]string{string(v.ID): v.IdentityHash}
		for _, a := range v.Anchors {
			current[string(v.ID)+"#"+a.Name] = a.IdentityHash
		}
		for name, hash := range current {
			if before, exists := old[name]; exists && before != hash {
				return fmt.Errorf("screen: published identity %s changed; declare a new reference", name)
			}
		}
	}
	return nil
}

// ParseSnapshot rejects unsupported versions, unknown or duplicate keys,
// malformed bindings and hashes. It returns no inventory on failure.
func ParseSnapshot(encoded []byte) (Inventory, error) {
	if err := uniqueJSON(encoded); err != nil {
		return Inventory{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var data SnapshotData
	if err := decoder.Decode(&data); err != nil {
		return Inventory{}, fmt.Errorf("screen: snapshot: %w", err)
	}
	if data.SchemaVersion != 1 || data.PolicyVersion != 1 {
		return Inventory{}, fmt.Errorf("screen: unsupported schema %d or policy %d", data.SchemaVersion, data.PolicyVersion)
	}
	views, mounts, err := restore(data)
	if err != nil {
		return Inventory{}, err
	}
	result, err := Publish(data.App, data.PolicyVersion, views, mounts)
	if err != nil {
		return Inventory{}, err
	}
	actual, _ := canonical(result.data)
	canonicalize(&data)
	expected, _ := canonical(data)
	if !bytes.Equal(actual, expected) {
		return Inventory{}, fmt.Errorf("screen: snapshot hashes, identities or canonical lists disagree")
	}
	return result, nil
}

func restore(data SnapshotData) ([]View, []Mount, error) {
	views := []View{}
	mounts := make([]Mount, 0, len(data.Operations))
	indices := map[string]int{}
	for _, op := range data.Operations {
		if _, exists := indices[op.ID]; exists {
			return nil, nil, fmt.Errorf("screen: duplicate operation %s", op.ID)
		}
		indices[op.ID] = len(mounts)
		mounts = append(mounts, Mount{Operation: op})
	}
	for _, v := range data.Views {
		spec := ViewSpec{Ref: v.ID, Cardinality: v.Cardinality, Hosts: v.Hosts}
		if v.Owner != v.ID.Owner() || v.Kind != v.ID.Kind() {
			return nil, nil, fmt.Errorf("screen: view %s owner or kind disagrees", v.ID)
		}
		for _, a := range v.Anchors {
			spec.Anchors = append(spec.Anchors, AnchorSpec{a.Name, a.Control, Binding{a.Binding}, a.Interactions})
		}
		view, err := NewView(spec)
		if err != nil {
			return nil, nil, err
		}
		views = append(views, view)
		for _, r := range v.Routes {
			index, exists := indices[r.OperationID]
			if !exists {
				return nil, nil, fmt.Errorf("screen: missing route operation %s", r.OperationID)
			}
			m := &mounts[index]
			if m.Operation != (Operation{r.OperationID, r.Surface, r.Method, r.Path, r.Guard}) || m.Owner != "" && m.Owner != v.Owner {
				return nil, nil, fmt.Errorf("screen: route %s disagrees with its operation", r.OperationID)
			}
			m.Owner, m.Document = v.Owner, true
			m.Responses = append(m.Responses, Response{v.ID, r.Response})
		}
	}
	return views, mounts, nil
}

func uniqueJSON(encoded []byte) error {
	d := json.NewDecoder(bytes.NewReader(encoded))
	d.UseNumber()
	if err := uniqueValue(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("screen: snapshot must contain exactly one JSON value")
	}
	return nil
}

func uniqueValue(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil
	}
	if !slices.Contains([]json.Delim{'{', '['}, delim) {
		return fmt.Errorf("screen: unexpected JSON delimiter")
	}
	keys := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || keys[name] {
				return fmt.Errorf("screen: duplicate or invalid JSON key %v", key)
			}
			keys[name] = true
		}
		if err := uniqueValue(d); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}
