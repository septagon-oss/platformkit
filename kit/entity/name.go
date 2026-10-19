package entity

// Noun is a resource's declared English singular and plural. The module's
// catalogue supplies the language-specific forms at render time.
//
// The type carries that name rather than the shorter one because a published
// OpenAPI document names a schema after the Go type, and the SCIM surface
// publishes the protocol's own name of a person under the key Name: two types
// called Name would be one component fighting for a name, and the document is
// rendered once for the whole application. Noun is the word the rest of the
// presentation contracts already use for the same idea.
type Noun struct {
	One   string `json:"one"`
	Other string `json:"other"`
}

// Name is Noun under the spelling every resource declaration writes.
type Name = Noun
