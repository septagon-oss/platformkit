package main

// Review 6's pins over the one door `2751741` opened, and over the doors the
// same commit's README says it did not.
//
// `httpx.AnyCredential()` is new in this kernel: a fifth authorisation
// declaration, and the reference composition uses it for exactly one route —
// `GET /api/v1/app/resources`, the document a client that is not a browser asks
// for first. The commit's argument for the kind is that the route spends no
// authority on the caller's own credentials, so it may answer whichever
// credential the caller holds; the mistake it enables is the wide one, and no
// boot-time gate refuses it, because whether an operation spends authority is
// not something a declaration checker can read off an operation id. So the
// guarantee has to be a case at the mount, and this is that case:
//
//  1. the door admits a caller who is nobody. `AnyCredential` is not `Public`,
//     and the kind exists precisely because the principal check still has to
//     run first. Measured: anonymous is 403 `AUTH_ANONYMOUS` today, and a
//     session is 200 — so the case cannot be satisfied by refusing everybody,
//     which is the shape the previous round's cure had.
//  2. the door belongs to a tenant. A key is minted inside one tenant and its
//     resolution reads a row inside the tenant the Host resolved; presented at
//     another tenant's address it is a stranger, and this door — new, and the
//     widest thing a key may now touch — must agree with every other door about
//     that. Reached only through statuses and the resource paths a document
//     names.
//  3. what the door answers is a promise it can keep. The document says "the
//     resources this caller may reach", so every path it names has to answer
//     that caller, and a read-only key must not be handed verbs. This is the
//     consistency between the catalogue's closures (`Readable`, `Writable`,
//     `CommandsFor`) and the middleware that guards the routes: `mayUse` answers
//     `recognised` for a `signed_in` command while `authorize` refuses a scoped
//     credential at such a route, so the two disagree today only in principle —
//     no registered resource declares one. The case runs both and would see it
//     the day someone mounts one.
//  4. the doors the README says are still a session's work are. `modules/auth/README.md`
//     lists them twice and names two more (`POST /logout` and
//     `POST /tokens/{id}/revoke`) with a reason each; `61e5f34`'s guard is what
//     makes them true, and nothing pinned five of the seven.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// r6CatalogPath is the kernel's own mount, the one AnyCredential door in the
// installation.
const r6CatalogPath = "/api/v1/app/resources"

// r6KeyScopedToARead mints the ordinary credential the brief's item 4 is about:
// a person's own key, narrowed to one read permission.
func r6KeyScopedToARead(t *testing.T, cfg config.Config, admin *http.Client) string {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, "/api/v1/auth/tokens",
		`{"name":"Review 6","scopes":["task:read"]}`)
	if code != http.StatusCreated {
		t.Fatalf("minting a key scoped to task:read = %d %s, want 201", code, body)
	}
	return field(t, body, "token")
}

// TestTheDoorEveryCredentialMayUseStillRefusesNobody proves the fifth kind is a
// credential door and not an open one, and that it did not become "answer
// anybody" on its way in — which is the shape a fix for "the key cannot read the
// catalog" could take and would then hand the installation's vocabulary to an
// anonymous request. The 200 half is the guard: the door does answer a caller.
func TestTheDoorEveryCredentialMayUseStillRefusesNobody(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodGet, acmeHost, r6CatalogPath, "")
	if code != http.StatusOK {
		t.Fatalf("the catalogue through a session = %d %s, want 200: without this half the case below "+
			"could be bought by refusing everybody", code, trimHTML(body))
	}

	anonymous := &http.Client{}
	code, body = do(t, cfg, anonymous, http.MethodGet, acmeHost, r6CatalogPath, "")
	if code < http.StatusBadRequest {
		t.Errorf("an anonymous GET %s = %d %s, want a refusal: httpx.AnyCredential admits any caller the "+
			"installation recognised, and an anonymous caller is nobody it recognised. The kernel's own "+
			"surfaces gate refuses this declaration on the Public router for exactly this reason; on the "+
			"workspace it is the principal check in authorize.go that has to hold.", r6CatalogPath, code, trimHTML(body))
	}
}

// TestTheDoorEveryCredentialMayUseRefusesAnotherTenantsKey presents acme's key at
// globex's address. The answer every other door gives is that the key resolves to
// nothing, because the row behind it is read inside the tenant the Host named —
// the new door reads the same resolution and has to give the same answer, or the
// widest thing a key may touch is also the one that got away with the difference.
func TestTheDoorEveryCredentialMayUseRefusesAnotherTenantsKey(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("creating the second tenant = %d %s", code, body)
	}
	key := r6KeyScopedToARead(t, cfg, admin)

	// Reachability, through what correct behaviour prints: the key is a working
	// credential at its own tenant, before any claim about the other one.
	if code, body = doKey(t, cfg, key, http.MethodGet, acmeHost, r6CatalogPath, ""); code != http.StatusOK {
		t.Fatalf("acme's own key reading the catalogue at acme.localhost = %d %s, want 200", code, trimHTML(body))
	}
	if code, _ := doKey(t, cfg, key, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusOK {
		t.Fatalf("acme's own key at %s = %d, want 200 before the claim about another tenant", tasksPath, code)
	}

	code, body = doKey(t, cfg, key, http.MethodGet, globexHost, r6CatalogPath, "")
	if code < http.StatusBadRequest {
		t.Errorf("acme's key reading %s at %s = %d %s, want a refusal: the document is built from the "+
			"caller's grants inside the tenant the Host resolved, and a key whose row lives in acme resolves "+
			"nowhere here — which is what every other door in this installation already says about the same "+
			"credential. A 200 here is a credential answering at a tenant it was never minted for.",
			r6CatalogPath, globexHost, code, trimHTML(body))
	}
}

// r6Catalogue is the document, as far as this case reads it: which resources are
// named, and which verbs of theirs.
type r6Catalogue struct {
	Resources []struct {
		Module   string `json:"module"`
		Entity   string `json:"entity"`
		Path     string `json:"path"`
		Writable bool   `json:"writable"`
		Commands []struct {
			Verb     string `json:"verb"`
			Endpoint string `json:"endpoint"`
		} `json:"commands"`
	} `json:"resources"`
}

// TestWhatTheCatalogueTellsAKeyTheKeyCanActuallyWalk reads the document with a
// key scoped to one read and then asks the API the same questions the document
// answers. Every path named has to answer that key, and a key scoped to a read
// must not be handed a verb: the catalogue's own documentation promises that
// "what somebody may not do, they are not told about", and the closures that
// decide it are not the code that guards the routes.
func TestWhatTheCatalogueTellsAKeyTheKeyCanActuallyWalk(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	key := r6KeyScopedToARead(t, cfg, admin)

	code, body := doKey(t, cfg, key, http.MethodGet, acmeHost, r6CatalogPath, "")
	if code != http.StatusOK {
		t.Fatalf("a key scoped to task:read reading %s = %d %s, want 200", r6CatalogPath, code, trimHTML(body))
	}
	var doc r6Catalogue
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		t.Fatalf("the catalogue is not the document a shell parses: %v\n%s", err, body)
	}
	if len(doc.Resources) == 0 {
		t.Fatalf("the key was shown no resources at all: the case below would pass on an empty answer, "+
			"and a shell with no vocabulary is not what a key is for. %s", body)
	}
	var named bool
	for _, r := range doc.Resources {
		if r.Path == tasksPath {
			named = true
		}
		if code, got := doKey(t, cfg, key, http.MethodGet, acmeHost, r.Path, ""); code >= http.StatusBadRequest {
			t.Errorf("the catalogue told a key scoped to task:read it may reach %s (%s/%s) and that read = %d %s: "+
				"Readable answered the closure one way and the route answered the other.",
				r.Path, r.Module, r.Entity, code, trimHTML(got))
		}
		if r.Writable {
			t.Errorf("the catalogue says %s is writable by a key scoped to task:read: Writable consults the "+
				"credential's own ceiling and should answer false here (%s)", r.Path, body)
		}
		for _, cmd := range r.Commands {
			// A command is refused for one of many reasons — no such row, wrong
			// state, an argument missing. It is never refused for lack of the
			// permission or the credential, because then the document advertised
			// a door the middleware shuts.
			where := cmd.Endpoint
			if where == "" {
				where = r.Path + "/{id}/" + cmd.Verb
			}
			code, got := doKey(t, cfg, key, http.MethodPost, acmeHost, where, `{}`)
			if code == http.StatusForbidden && strings.Contains(got, "AUTH_DENIED") {
				t.Errorf("the catalogue offered %s to a key scoped to task:read and the route refused it for "+
					"the credential: CommandsFor and authorize.go disagree about the same declaration (%s).",
					where, trimHTML(got))
			}
		}
	}
	if !named {
		t.Errorf("the catalogue a key scoped to task:read was shown names no %s among %d resources: %s",
			tasksPath, len(doc.Resources), body)
	}
}

// TestTheSelfServiceDoorsTheReadmePromisesAreStillASessionsWork walks the list
// modules/auth/README.md gives twice — "mint a key, replace the recovery codes,
// withdraw a factor, revoke a session, sign out of every browser", plus
// `POST /logout` and `POST /tokens/{id}/revoke`, each with a sentence of its own
// about why a key may not do it — and asks a key for each. `61e5f34`'s guard is
// what makes those sentences true; review 5 pinned five doors and these seven
// are what the file promises, so the promise and the proof are now the same size.
func TestTheSelfServiceDoorsTheReadmePromisesAreStillASessionsWork(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	key := r6KeyScopedToARead(t, cfg, admin)

	// Reachability first, and it holds whatever the doors below do.
	if code, _ := doKey(t, cfg, key, http.MethodGet, acmeHost, tasksPath, ""); code != http.StatusOK {
		t.Fatalf("the key at %s = %d, want 200 before the doors a key is refused", tasksPath, code)
	}

	for _, door := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/v1/auth/logout", ``},
		{http.MethodPost, "/api/v1/auth/tokens", `{"name":"Wider","scopes":["task:read"]}`},
		{http.MethodGet, "/api/v1/auth/tokens", ``},
		{http.MethodPost, "/api/v1/auth/tokens/00000000-0000-0000-0000-000000000000/revoke", ``},
		{http.MethodGet, "/api/v1/auth/factors", ``},
		{http.MethodPost, "/api/v1/auth/factors/totp/begin", ``},
		{http.MethodDelete, "/api/v1/auth/factors/00000000-0000-0000-0000-000000000000", ``},
		{http.MethodPost, "/api/v1/auth/password", `{"current":"x","next":"y y y y y y y y y y y y"}`},
	} {
		code, got := doKey(t, cfg, key, door.method, acmeHost, door.path, door.body)
		if code < http.StatusBadRequest {
			t.Errorf("a key scoped to task:read at %s %s = %d %s, want a refusal: modules/auth/README.md says "+
				"these are \"a session's work and never a key's\" and names %s as httpx.SignedIn() work on the "+
				"caller's own credentials.", door.method, door.path, code, trimHTML(got), door.path)
		}
	}
}
