package legible

import "testing"

// Exempt lets any token holding a slash pass as an address ("Nothing written as copy
// needs a slash to say it"). Copy does: a toggle's label, a column that reads "N/A", a
// choice between two words. Each of these is a sentence somebody wrote in Go and a
// translator must be shown, and none is a path, a media type or a URL — while the
// addresses beside them stay data.
func TestAChoiceWrittenWithASlashIsCopy(t *testing.T) {
	for _, text := range []string{"On/Off", "Yes/No", "N/A", "Show/Hide", "Read/Write access"} {
		if Exempt(text) {
			t.Errorf("Exempt(%q) = true; a label written with a slash is copy, not an address", text)
		}
	}
	for _, text := range []string{"/app/task/tasks", "text/plain", "https://acme.localhost/app"} {
		if !Exempt(text) {
			t.Errorf("Exempt(%q) = false; an address is data", text)
		}
	}
}
