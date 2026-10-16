package main

// A retired customer, asked for one more change: the control plane's answer to every
// lifecycle verb it is asked of after the delete, at the route a person or a shell
// actually calls, plus what the database holds when the eleven refusals are over.
//
// The invariant is the one every command here repeats — a refusal commits nothing,
// emits nothing and returns no stale row — and `delete` is the state in which it is
// hardest to hold, because the verb releases the two names the platform routes on
// (the slug, by the partial index of migrations/000006, and the rows in the global
// PRIMARY KEY `tenant_hosts`) while leaving the tenant's own row behind. A command
// that read a tenant without `deleted_at IS NULL` would therefore still find that
// row, and `reactivate` would answer a suspended row with `status:"active"` for a
// customer that has no hosts left to answer at: an active tenant that `List` does not
// list, that `ByHost` cannot resolve, and that no screen can reach. That is a row
// whose state no reader agrees about, which is the failure the delete comment says a
// restore would have to fix.
//
// What already covers this, and what this adds:
// `TestALifecycleVerbThatRacesADeleteFindsNoTenantToActOn` drives `Suspend` and
// `AddHost` concurrently with a `Delete` through the service, and
// `TestEachLifecycleVerbAnswersAtItsOwnRoute` asserts the retired tenant's *read*
// answers 404 once, after a delete that it also used to release a hostname. Between
// them no case asks the eleven doors — including the one that writes a user into the
// customer, `invite`, which is the verb a retired tenant must never gain — of one
// retired customer in sequence, and none says that the refusals left no outbox row
// behind. The outbox is read rather than the trail on purpose: an event is written in
// the command's own transaction and reaches `audit_events` only when the relay runs,
// so the trail would make this case wait on somebody else's schedule to prove a
// command that wrote nothing at all.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// retiredState is the row as the database holds it: the columns the eleven verbs
// would move, read from behind the HTTP answer.
type retiredState struct {
	Name       string `gorm:"column:name"`
	Status     string `gorm:"column:status"`
	Deleted    bool   `gorm:"column:retired"`
	Hosts      int64  `gorm:"column:hosts"`
	OutboxRows int64  `gorm:"column:outbox"`
}

// readRetired asks the database, in one system transaction, what the customer's row
// and its two satellite tables hold. `platformkit_outbox` is the queue a refused
// command would have had to write into to emit anything, and it is written in the
// command's own transaction, so this count moves exactly when a verb emits.
func readRetired(t *testing.T, cfg config.Config, customer uuid.UUID) retiredState {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	defer conn.Close()

	var out retiredState
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if err := tx.DB().Table("tenants").
			Select("name, status, deleted_at IS NOT NULL AS retired").
			Where("id = ?", customer).Take(&out).Error; err != nil {
			return err
		}
		if err := tx.DB().Table("tenant_hosts").Where("tenant_id = ?", customer).
			Count(&out.Hosts).Error; err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").Where("tenant_id = ?", customer).
			Count(&out.OutboxRows).Error
	})
	if err != nil {
		t.Fatalf("read the retired customer back: %v", err)
	}
	return out
}

func TestEveryLifecycleVerbRefusesATenantTheControlPlaneRetired(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"umbrella","name":"Umbrella Corporation","host":"umbrella.localhost"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s = %d %s, want 201", tenantPath, code, body)
	}
	customer := uuid.MustParse(field(t, body, "id"))

	// The bystander, created before the retirement and never requested: `Delete`
	// releases names by writing to a table whose `host` column is a global key, so
	// the question a reviewer can ask of that DELETE is whose rows it removes. A
	// release that reached past the tenant in hand would take the customer beside
	// this one off the internet — and the installation's own host with it, since
	// `acme.localhost` is a row in the same table.
	if code, body = do(t, cfg, admin, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"kontiki","name":"Kontiki Limited","host":"kontiki.localhost"}`); code != http.StatusCreated {
		t.Fatalf("POST %s for the bystander = %d %s, want 201", tenantPath, code, body)
	}
	bystander := uuid.MustParse(field(t, body, "id"))
	if got := hostRows(t, cfg, bystander); got != 1 {
		t.Fatalf("the bystander holds %d host rows before the delete, want the one its create attached", got)
	}

	at := func(method, suffix, req string) (int, string) {
		t.Helper()
		return do(t, cfg, admin, method, acmeHost, tenantPath+"/"+customer.String()+suffix, req)
	}

	// The control, through the behaviour that works: these doors answer this
	// customer while it is served, so a 404 below is the retirement answering and
	// not a route nobody mounted or an address the installation never serves.
	if code, body = at(http.MethodPost, "/rename", `{"name":"Umbrella Industries"}`); code != http.StatusOK {
		t.Fatalf("POST rename before the delete = %d %s, want 200", code, body)
	}
	if code, body = at(http.MethodPost, "/hosts", `{"host":"umbrella.example.com"}`); code != http.StatusCreated {
		t.Fatalf("POST add-host before the delete = %d %s, want 201", code, body)
	}
	if code, body = at(http.MethodPost, "/suspend", ""); code != http.StatusOK {
		t.Fatalf("POST suspend before the delete = %d %s, want 200", code, body)
	}
	if code, body = at(http.MethodPost, "/delete", `{"confirm":"umbrella"}`); code != http.StatusOK {
		t.Fatalf("POST delete with the slug repeated = %d %s, want 200", code, body)
	}

	// The released pair is gone from the routing table and the row is retired: this
	// is the state every verb below is asked of, read from the database rather than
	// from the delete's own body.
	before := readRetired(t, cfg, customer)
	if !before.Deleted || before.Hosts != 0 {
		t.Fatalf("the delete left the customer retired=%v with %d host rows, want retired and no name held",
			before.Deleted, before.Hosts)
	}
	if before.OutboxRows == 0 {
		t.Fatalf("the four verbs this customer went through left no outbox row: the case has nothing to compare")
	}

	// Every door, including the one that writes a person into the customer: a
	// retired tenant does not get a first administrator either. Each body is one the
	// decoder would take and the service would act on for a tenant still served, so
	// the answer below is the surface's own.
	for _, probe := range []struct{ method, at, body string }{
		{http.MethodGet, "", ""},
		{http.MethodPost, "/rename", `{"name":"Umbrella Renewed"}`},
		{http.MethodPost, "/reactivate", ""},
		{http.MethodPost, "/suspend", ""},
		{http.MethodPost, "/hosts", `{"host":"umbrella.example.net"}`},
		{http.MethodDelete, "/hosts/umbrella.localhost", ""},
		{http.MethodPost, "/locale", `{"default":"en","supported":["en"]}`},
		{http.MethodPost, "/delete", `{"confirm":"umbrella"}`},
		{http.MethodPost, "/invite", `{"email":"first@umbrella.localhost","displayName":"First Administrator"}`},
	} {
		if code, body = at(probe.method, probe.at, probe.body); code != http.StatusNotFound {
			t.Errorf("%s %s%s of a retired customer = %d %s, want 404: a retired tenant is not found, "+
				"by this verb as by every read", probe.method, tenantPath, probe.at, code, body)
		}
	}

	// Nothing was committed and nothing was emitted: the row is the retirement the
	// delete wrote, no name came back to the routing table, and the outbox holds the
	// same rows the four accepted verbs wrote — a refusal that published anything
	// would put an act in a trail for a change nobody made.
	after := readRetired(t, cfg, customer)
	if after.Name != before.Name || after.Status != before.Status || !after.Deleted {
		t.Errorf("a refused lifecycle verb moved the retired customer to %q/%q retired=%v, want %q/%q retired=true",
			after.Name, after.Status, after.Deleted, before.Name, before.Status)
	}
	if after.Hosts != before.Hosts {
		t.Errorf("the refused verbs left %d host rows for a retired customer, want %d: a name attached by a "+
			"command that refused is a hostname nobody owns", after.Hosts, before.Hosts)
	}
	if after.OutboxRows != before.OutboxRows {
		t.Errorf("the refused verbs left %d outbox rows beside the %d the accepted verbs wrote, want %d: "+
			"a refusal emits nothing", after.OutboxRows, before.OutboxRows, before.OutboxRows)
	}

	// And the release stayed inside the customer it was asked about. Every tenant of
	// an installation is served at a name in one global table, so the DELETE that
	// hands a retired customer's names to the next one is scoped by the row it owns
	// and not by the name it was given; this is the one fact about it that no count
	// of the retired tenant's own rows can say.
	if got := hostRows(t, cfg, bystander); got != 1 {
		t.Errorf("deleting one customer left its bystander with %d host rows, want the one it was created "+
			"with: the release reached past the tenant the request named", got)
	}
}

// hostRows is the count of one tenant's rows in the table that says which customer
// a request belongs to. It is read from the database rather than by requesting the
// bystander's address because a resolved host is believed for `hostTTL` (30 s) and
// `Delete` invalidates only the names the retired tenant held: a release that
// reached past its own tenant would keep answering from the cache while the row
// behind it was already gone. The row is the fact; the cache is a copy of it.
func hostRows(t *testing.T, cfg config.Config, tenant uuid.UUID) int64 {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	defer conn.Close()

	var n int64
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("tenant_hosts").Where("tenant_id = ?", tenant).Count(&n).Error
	})
	if err != nil {
		t.Fatalf("count the host rows of %s: %v", tenant, err)
	}
	return n
}
