package entity

// Name is a resource's declared English singular and plural. The module's
// catalogue supplies the language-specific forms at render time.
type Name struct {
	One   string `json:"one"`
	Other string `json:"other"`
}
