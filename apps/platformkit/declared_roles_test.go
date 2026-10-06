package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestEveryDeclaredRoleReachesTheScreensItsModuleOwns is rule 3's other half, read
// off the manifests rather than written out here. modules/auth's seed gives every
// tenant `admin` and `member`, and the composition seeds the roles the composed
// modules declare (apps/platformkit's seedRoles takes them from
// declaredRoles(mods)); a role somebody can grant through the roles screen has to
// do what its module's nav entry promises, in this composition, or the grant is a
// tick that changes nothing — which is the services-law finding in the walkthrough
// of record, and the reason the declaration exists.
//
// The table is therefore built from compose(cfg).modules: one row per
// (declared role × a nav entry of the module that declares it), and one refusal
// row per (declared role × a nav entry of a module that does not). Nothing here
// lists a role or a screen, so a module that declares a third role or a second
// screen is covered the day it does it, by this case and not by an edit to it.
//
// The positive rows are the finding. The refusal rows are its second half: a
// refusal is only a refusal when it says what is missing, and
// kit/httpx/authorize.go and ui/page/fault.go claim they do.
func TestEveryDeclaredRoleReachesTheScreensItsModuleOwns(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	acme := acmeTenant(t, cfg)
	named := 0
	for _, m := range c.modules {
		if len(m.Roles) == 0 {
			continue
		}
		for _, role := range m.Roles {
			// One person per role, holding exactly it: provisionAs writes the row
			// with the role attached, which is what granting it through the roles
			// screen ends up doing to somebody.
			email := role.Name + ".declared@acme.localhost"
			provisionAs(t, cfg, c, acme, email, role.Name)
			as := signIn(t, cfg, acmeHost, email, adminPass)

			for _, entry := range m.Nav {
				screen := httpx.Workspace(entry.Screen)
				code, body := do(t, cfg, as, http.MethodGet, acmeHost, screen, "")
				if code != http.StatusOK {
					t.Errorf("%s holds the %s role %s declares, and GET %s = %d %s: the grant opens nothing",
						email, role.Name, m.Name, screen, code, brief(body))
				}
			}

			// What this role does not open has to refuse, and say which permission
			// it lacks. The control plane of an installation this tenant is not the
			// operator of answers 404 rather than 403 — the same rule
			// TestEveryPersonaCanDoItsJourneysAndIsRefusedTheOthers holds, and for
			// the same reason: a 200 would be the leak and either refusal is the
			// door.
			for _, other := range c.modules {
				if other.Name == m.Name {
					continue
				}
				for _, entry := range other.Nav {
					screen := httpx.Workspace(entry.Screen)
					code, body := do(t, cfg, as, http.MethodGet, acmeHost, screen, "")
					switch {
					case code == http.StatusForbidden:
						if !strings.Contains(body, entry.Permission) {
							t.Errorf("%s is refused %s without naming what is missing (403 says nothing about %s): %s",
								email, screen, entry.Permission, brief(body))
						}
						named++
					case code == http.StatusNotFound:
					default:
						t.Errorf("%s holds only %s, and GET %s = %d %s, want a refusal that names the permission",
							email, role.Name, screen, code, brief(body))
					}
				}
			}
		}
	}
	// The generated table refusing something is the case's own proof: a
	// composition where every declared role reached every screen would be a
	// composition in which grants mean nothing, and this test would be green
	// while saying nothing about the finding it exists for.
	if named == 0 {
		t.Error("no declared role was refused a screen belonging to another module: the grants in this composition are not being checked")
	}
}

// brief is the first 200 bytes of a response. A screen that answers with HTML is
// forty kilobytes, which is more report than a failure needs: what decides the
// case is the code and the permission, and a wall of markup buries both.
func brief(body string) string {
	r := []rune(strings.Join(strings.Fields(body), " "))
	if len(r) <= 200 {
		return string(r)
	}
	return string(r[:200]) + "…"
}
