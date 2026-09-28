package porttest

import "reflect"

// detached is a copy of a row that shares no storage with the row it came from.
//
// Assignment copies a struct's fields, and a slice, map or pointer field is a
// header over storage the original also names — so a fake that hands a command an
// assigned copy hands it the stored row. That is the direction a refusal travels:
// `Do` checks what a command says after the command decided what to write, because
// only the command knows which events its write is news about, and an undeclared
// name then refuses a write whose header-backed fields already landed. The tenant
// is left holding a change no event says and no commit covers, which is the half of
// "domain state and what the command says commit together" a `bool` field cannot
// show and a `[]NavItem` shows at once. A database gives a command the row it read,
// not an alias of the page that holds it, and this is the same courtesy: what a
// command mutates is its own until the store takes it.
//
// Two limits are the row type's own, and saying them is cheaper than a mechanism
// that hides them. A field the type does not export is copied as assignment leaves
// it — reaching past it needs unsafe, which no test kernel has a claim on — so a row
// whose unexported field is a header shares that storage with whoever holds the row;
// `uuid.UUID` and `time.Time` are value fields as far as this cares. And the walk
// follows what a row points at, which for a row means one level of what it owns: a
// type that points back at itself is not a row, and this would follow it forever.
func detached[T any](row T) T {
	v := reflect.ValueOf(row)
	if !v.IsValid() || !sharesStorage(v.Type()) {
		return row
	}
	return copyStorage(v).Interface().(T)
}

// sharesStorage answers whether a value of this type is a header over storage
// another value can reach. It is the fast path's question, and the common answer is
// no: a row whose fields are all values is already a copy, and the walk below costs
// it nothing. Struct fields the type does not export are left out, because the walk
// cannot rewrite them and promising what it cannot do would be worse.
func sharesStorage(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Map, reflect.Pointer, reflect.Interface:
		return true
	case reflect.Array:
		return sharesStorage(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			if t.Field(i).IsExported() && sharesStorage(t.Field(i).Type) {
				return true
			}
		}
	}
	return false
}

// copyStorage returns a copy of v that shares no storage with it, keeping what a
// row is made of rather than its shape: a nil slice or map stays nil rather than
// becoming empty, because a module's Snapshot renders those two differently and a
// copy that "improved" one into the other would move a case's expectation. The
// fields it cannot reach are the unexported ones `sharesStorage` already named.
func copyStorage(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(copyStorage(v.Elem()))
		return out
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(copyStorage(v.Elem()))
		return out
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(copyStorage(v.Index(i)))
		}
		return out
	case reflect.Array:
		if !sharesStorage(v.Type()) {
			return v
		}
		out := reflect.New(v.Type()).Elem()
		for i := 0; i < v.Len(); i++ {
			out.Index(i).Set(copyStorage(v.Index(i)))
		}
		return out
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeMapWithSize(v.Type(), v.Len())
		for iter := v.MapRange(); iter.Next(); {
			out.SetMapIndex(copyStorage(iter.Key()), copyStorage(iter.Value()))
		}
		return out
	case reflect.Struct:
		out := reflect.New(v.Type()).Elem()
		out.Set(v)
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			if !t.Field(i).IsExported() || !sharesStorage(t.Field(i).Type) {
				continue
			}
			out.Field(i).Set(copyStorage(v.Field(i)))
		}
		return out
	default:
		return v
	}
}
