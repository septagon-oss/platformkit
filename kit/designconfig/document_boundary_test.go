package designconfig_test

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/designconfig"
)

func TestClientDesignRefusesUnreadDocuments(t *testing.T) {
	const valid = "slug: lantern\nseed:\n  sector: publishing\n  name: Lantern\n"
	for _, tc := range []struct {
		name, suffix string
		refuse       bool
	}{
		{"one document", "", false},
		{"trailing comment", "# End of the identity\n", false},
		{"second identity", "---\nslug: other\nseed: {sector: publishing, name: Other}\n", true},
		{"unread override", "---\ntokens:\n  light:\n    text-primary: '#ffffff'\n", true},
		{"unread unknown field", "---\nmotion: unlimited\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := fstest.MapFS{"lantern/design.yaml": &fstest.MapFile{Data: []byte(valid + tc.suffix)}}
			loaded, err := designconfig.LoadClientDesign(files, "lantern")
			if tc.refuse {
				if err == nil {
					t.Errorf("accepted a file with an unread YAML document as client %q", loaded.Slug())
				}
				pairs, setErr := designconfig.LoadClientDesigns(files)
				if setErr == nil || len(pairs) != 0 {
					t.Errorf("set loader returned %d identities, error %v; want a refusal and no identities", len(pairs), setErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			register := designconfig.NewRegister()
			if err := register.Add(loaded); err != nil {
				t.Fatal(err)
			}
		})
	}
}
