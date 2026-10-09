package main

// SPECIFY §8 C6, and the sentence in modules/user/README.md that says a refusal is
// on the trail: the record has to be *readable* afterwards, through the trail's own
// query, with the caller and the call that made it. The three composition cases below
// ask the invariant from the service side and count the outbox rows a refusal leaves;
// a count of the outbox is a count of what was published, not of what somebody can
// later ask for. The difference is the detached transaction: the event is written
// outside the caller's, on the pool connection kit/httpx put on the request, keeping
// the principal, the request id and the trace while dropping the pending transaction.
// What this case proves is the whole of that decision — the row a refused write
// publishes is relayed, subscribed by modules/audit and answered by GET /api/v1/audit
// /events with the actor credited and the request named — and, beside it, that the
// two grant lists in the auth module's payload really are the refusal: what the role
// granted before the write, and what the write would have left.
//
// It is the same chain TestAnErasureLeavesOneAuditRowNamingTheDigestThatIsGone reads
// for a file, reached from the one door that makes a refusal worth recording: an
// administrator at her own tenant, twice refused, and still administrator afterwards.

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestARefusedAdministrationWriteIsInTheTrail(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	// The trail is a plan feature in this product, so reading it is at the door that
	// feature gates: the subscription below is what app_test.go's own case buys.
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, plansWrite,
		`{"code":"pro","name":"Pro","priceCents":2900,"currency":"EUR","interval":"month","active":true,"features":["audit-trail"]}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", plansWrite, code, body)
	}
	planID := field(t, body, "id")
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, subPath+"/subscribe", `{"planId":"`+planID+`"}`); code != http.StatusOK {
		t.Fatalf("subscribe = %d %s, want 200", code, body)
	}

	// The bootstrap administrator is this tenant's only administering person, so the
	// next write at either door is the one that takes the last one away.
	me := field(t, whoami(t, cfg, admin), "userId")

	// Door one: her own roles, at the door her own grant guards.
	rolesID := refusedWrite(t, cfg, admin, http.MethodPost, usersPath+"/"+me+"/roles", `{"roles":["member"]}`)
	// Door two: the grants of the role that makes her one. What the role grants is read
	// at the roles door before the write, so the record below is compared with the state
	// rather than with a list copied into this file.
	granted := grantsAtTheDoor(t, cfg, admin, authcontracts.RoleAdmin)
	grantsID := refusedWrite(t, cfg, admin, http.MethodPut, "/api/v1/auth/roles/"+authcontracts.RoleAdmin,
		`{"permissions":[]}`)

	userRow := waitForAudit(t, cfg, admin, usercontracts.EventAdministrationRefused)
	if got := userRow["actor"]; got != me {
		t.Errorf("the trail credits the refused roles edit to %v, want the administrator %s", got, me)
	}
	if got := userRow["requestId"]; got != rolesID {
		t.Errorf("the refused roles edit is answered with requestId %v, want the call she was told was %s", got, rolesID)
	}
	userPayload := trailPayload(t, userRow)
	if got := userPayload["userId"]; got != me {
		t.Errorf("the refused roles edit names userId %v, want %s", got, me)
	}
	if got := userPayload["attempt"]; got != "roles" {
		t.Errorf("the refused write at the person's door is recorded as attempt %v, want roles", got)
	}
	if got := listedRoles(t, userPayload["roles"]); !strings.Contains(got, authcontracts.RoleAdmin) {
		t.Errorf("the record names %q as the grants the write would have taken away, want the role that makes her an administrator", got)
	}

	authRow := waitForAudit(t, cfg, admin, authcontracts.EventAdministrationRefused)
	if got := authRow["requestId"]; got != grantsID {
		t.Errorf("the refused grant edit is answered with requestId %v, want the call she was told was %s", got, grantsID)
	}
	authPayload := trailPayload(t, authRow)
	if got := authPayload["role"]; got != authcontracts.RoleAdmin {
		t.Errorf("the refused grant edit names role %v, want %s", got, authcontracts.RoleAdmin)
	}
	if was := listedRoles(t, authPayload["was"]); was != granted {
		t.Errorf("the record says role %s granted %q before the write, want the %q it grants at its own door",
			authcontracts.RoleAdmin, was, granted)
	}
	if now := listedRoles(t, authPayload["now"]); now != "" {
		t.Errorf("the record says the refused write would have left the role granting %q, want nothing", now)
	}

	// And the two refusals refused: the same person, at the same door, still holding the
	// role and its grants — the trail is a record of what did not happen, and the state it
	// describes is the state that did.
	if code, body = do(t, cfg, admin, http.MethodGet, acmeHost, usersPath+"/"+me, ""); code != http.StatusOK ||
		!strings.Contains(body, authcontracts.RoleAdmin) {
		t.Fatalf("the person after two refusals = %d %s, want her still holding %s", code, body, authcontracts.RoleAdmin)
	}
	if now := grantsAtTheDoor(t, cfg, admin, authcontracts.RoleAdmin); now != granted {
		t.Errorf("role %s grants %q after the refusal, want %q: a refused write leaves its row alone",
			authcontracts.RoleAdmin, now, granted)
	}
}

// refusedWrite makes one write the rule is expected to stop and returns the request
// id its caller was answered with. The id is the point: it is the only handle the
// caller keeps, and the trail's promise is that it answers to that handle.
func refusedWrite(t *testing.T, cfg config.Config, client *http.Client, method, path, body string) string {
	t.Helper()
	req, err := http.NewRequest(method, "http://"+cfg.Server.Addr+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/json")
	res, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("%s %s = %d %s, want 422: the case is about a refusal this rule is meant to make",
			method, path, res.StatusCode, out)
	}
	id := res.Header.Get("X-Request-ID")
	if id == "" {
		t.Fatalf("%s %s refused with no %s header: nothing to ask the trail about", method, path, "X-Request-ID")
	}
	return id
}

// trailPayload is the event's own payload as the trail answered it, which is what the
// module published and not a second copy of anything.
func trailPayload(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(row["payload"])
	if err != nil {
		t.Fatalf("read the payload of %v: %v", row, err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the trail's payload is not an object: %s", raw)
	}
	return out
}

// listedRoles joins a payload's list of names in the order nobody promised, and returns
// the empty string for a list that is empty, absent or null — which is what a refused
// write's "after" is: the role grants nothing, and the record says so by carrying
// nothing. Sorted, because the two lists compared with it come out of a text[] column
// and a JSON array, and only their contents are a fact.
func listedRoles(t *testing.T, value any) string {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		if s, isString := item.(string); isString {
			names = append(names, s)
		}
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

// grantsAtTheDoor is one role's grants, read at the door that lists them. The refusal record
// carries the same list as `was`; this is where the case gets it instead of writing a
// specialist fact into a test file.
func grantsAtTheDoor(t *testing.T, cfg config.Config, client *http.Client, name string) string {
	t.Helper()
	code, body := do(t, cfg, client, http.MethodGet, acmeHost, "/api/v1/auth/roles", "")
	if code != http.StatusOK {
		t.Fatalf("GET /api/v1/auth/roles = %d %s, want 200 to the person holding the role", code, body)
	}
	var out struct {
		Items []struct {
			Name        string   `json:"name"`
			Permissions []string `json:"permissions"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("read the roles from %s: %v", body, err)
	}
	for _, role := range out.Items {
		if role.Name == name {
			grants := slices.Clone(role.Permissions)
			slices.Sort(grants)
			return strings.Join(grants, ",")
		}
	}
	t.Fatalf("no role %q in %s", name, body)
	return ""
}
