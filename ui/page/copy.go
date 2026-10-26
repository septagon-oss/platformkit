package page

import (
	"encoding/json"
	"sync"
)

// ownCopy is this package's own copy, read from the file copy lives in rather than
// written at the call site, once, lazily, and never mutated.
//
// Copy lives in a catalogue file rather than at the call site, and a label is
// copy: "Try again" is a decision about a page and not a fact about this program,
// so the deployment that re-words its Portuguese refusal should be able to re-word
// the English label under the same key, rather than one glued into the binary at
// compile time.
//
// The gettext rule still holds for the sentences this package did not write. A
// guard's line carries something only the refusal knows, so the source-language copy
// of those keys is the guard's own sentence and no static entry could carry it —
// which is why messages/en.json carries exactly the lines fault.go and access.go
// write themselves. The catalogue gate refuses the two lists drifting.
var ownCopy = sync.OnceValue(func() map[string]string {
	body, err := catalogues.ReadFile("messages/en.json")
	if err != nil {
		// The catalogue is embedded, so this is a broken build rather than a
		// runtime condition, and Catalogue refuses on the same ground.
		panic("ui/page: its source-language catalogue is not in the binary: " + err.Error())
	}
	var file map[string]struct {
		Translation string `json:"translation"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		panic("ui/page: messages/en.json is not gotext JSON: " + err.Error())
	}
	lines := make(map[string]string, len(file))
	for key, message := range file {
		lines[key] = message.Translation
	}
	return lines
})

// own is one line of this package's copy in its source language.
func own(key string) string { return ownCopy()[key] }
