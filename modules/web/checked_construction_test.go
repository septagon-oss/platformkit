package web_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
	"github.com/septagon-oss/platformkit/modules/site/contracts/sitetest"
	"github.com/septagon-oss/platformkit/modules/web"
)

// TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence: the site renders two modules' rows and
// links two addresses it does not serve — a page missing either would draw a link
// that leads nowhere.
// fileURL is where a stored picture is fetched from.
var fileURL = func(id string) string { return "/f/" + id }

func full() web.Deps {
	return web.Deps{Site: sitetest.NewFake(), Content: contenttest.NewFake(), SignInPath: "/login", PublicFileURL: fileURL}
}
func TestNewCheckedNamesTheRefusalAndNewPanicsWithTheSameSentence(t *testing.T) {
	rows := []struct {
		name string
		deps web.Deps
		want string
	}{
		{"neither service", web.Deps{},
			"web: Deps.Site and Deps.Content are required"},
		{"the content missing beside the site", web.Deps{Site: sitetest.NewFake(), SignInPath: "/login", PublicFileURL: fileURL},
			"web: Deps.Site and Deps.Content are required"},
		{"no sign-in route to link", web.Deps{Site: sitetest.NewFake(), Content: contenttest.NewFake(), PublicFileURL: fileURL},
			"web: Deps.SignInPath and Deps.PublicFileURL are required"},
		{"no address a stored picture is fetched through", web.Deps{Site: sitetest.NewFake(), Content: contenttest.NewFake(), SignInPath: "/login"},
			"web: Deps.SignInPath and Deps.PublicFileURL are required"},
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			_, err := web.NewChecked(r.deps)
			if err == nil {
				t.Fatal("NewChecked built a site that could not render or could not link")
			}
			if !strings.Contains(err.Error(), r.want) {
				t.Errorf("refusal = %q, want it to contain %q", err, r.want)
			}
			var got any
			func() {
				defer func() { got = recover() }()
				web.New(r.deps)
			}()
			if s, ok := got.(string); !ok || s != err.Error() {
				t.Errorf("panicked with %#v, want the string the checked form answered %q", got, err)
			}
		})
	}
}

// TestNewCheckedBuildsFromItsRequiredDependenciesAlone: with the two services and
// the two addresses named, the theme, the copy and the translation legs stay the
// composition's choice.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	m, err := web.NewChecked(full())
	if err != nil {
		t.Fatalf("NewChecked with the site and the two addresses: %v", err)
	}
	if m.Name != "web" {
		t.Errorf("manifest name = %q, want web", m.Name)
	}
}
