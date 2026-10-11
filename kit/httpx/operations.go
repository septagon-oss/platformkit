package httpx

// operations.go names the five routes a collection resource can offer, so that
// a resource can say which of them it actually has.
//
// The five are not a new vocabulary: they are the last segment of the operation
// ids kit/rest already builds ("<module>-<entity>-<verb>") and the keys of its
// own `writes` map, which is what makes one verb mean the same thing in the
// route table, on a generated screen, and in the catalogue a phone parses.

// CRUD is one of the five routes a collection resource may offer. A resource
// states the ones it mounted; a verb it left out answers "nothing is served at
// this address", and draws no door.
type CRUD string

// The five verbs, in the order a shell lists them and the order the catalogue
// publishes them.
const (
	CRUDList   CRUD = "list"
	CRUDRead   CRUD = "read"
	CRUDCreate CRUD = "create"
	CRUDUpdate CRUD = "update"
	CRUDDelete CRUD = "delete"
)

// CRUDValues is the five in that order. An empty operation set *means* this
// list: every Spec written before a resource could name its operations offers
// all five, and says so by saying nothing.
func CRUDValues() []CRUD {
	return []CRUD{CRUDList, CRUDRead, CRUDCreate, CRUDUpdate, CRUDDelete}
}

// ValidCRUD reports whether verb is one of the five. kit/rest checks a Spec's
// operation set with it, so a typo names the vocabulary where it was written
// rather than mounting nothing and looking like a resource with no routes.
func ValidCRUD(verb string) bool {
	for _, c := range CRUDValues() {
		if string(c) == verb {
			return true
		}
	}
	return false
}

// Offers reports whether this resource mounted the route for a verb. An empty
// Operations answers true for all five.
//
// It is a property of the code that mounted the resource, never of who is
// asking: what differs per caller is whether they may use a route that exists,
// which is ReadAuth's and WriteAuth's question, and answered by Readable,
// Writable and CommandsFor.
func (r Resource) Offers(c CRUD) bool {
	if len(r.Operations) == 0 {
		return true
	}
	for _, o := range r.Operations {
		if o == c {
			return true
		}
	}
	return false
}

// OperationWords is this resource's operation set as the catalogue prints it —
// the five in order, and nil for a resource that offers all five, because an
// entry that hides no verb prints no list of them.
func (r Resource) OperationWords() []string {
	if len(r.Operations) == 0 {
		return nil
	}
	out := make([]string, 0, len(r.Operations))
	for _, c := range CRUDValues() {
		if r.Offers(c) {
			out = append(out, string(c))
		}
	}
	return out
}
