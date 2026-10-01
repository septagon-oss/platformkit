package main

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAnErasureLeavesOneAuditRowNamingTheDigestThatIsGone is pillar 3 read back
// instead of asserted. `modules/audit` records every event the installation
// publishes — that is its own module's tested behaviour — and this module's
// README and SPECIFY both say a retention or an erasure is therefore in the
// trail. Nothing here had ever read one of those rows back, which is the
// difference between a claim about a sink and a record somebody can be asked for.
//
// It runs the whole chain rather than a piece of it, because the pieces are the
// kind of thing that each work alone: a person deletes a file at the door
// `file:manage` guards; the row goes and `file.deleted` is published; the
// subscription removes the bytes after that transaction committed and certifies
// it; `file.erased` follows. Both records then have to name the digest the upload
// returned — and the digest is the whole point: by the time anybody reads the
// trail, the row it described is gone, so the only thing left that says *which*
// bytes were removed is what the event carried away with it.
func TestAnErasureLeavesOneAuditRowNamingTheDigestThatIsGone(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const content = "the evidence, which has to be describable after it is gone"
	code, uploaded := putFile(t, cfg, admin, filesPath, "evidence.txt", "text/plain", content)
	if code != http.StatusCreated {
		t.Fatalf("the upload = %d %s; this case cannot be asked without a file", code, uploaded)
	}
	id, digest := field(t, uploaded, "id"), field(t, uploaded, "sha256")

	if code, body := do(t, cfg, admin, http.MethodDelete, acmeHost, filesPath+"/"+id, ""); code != http.StatusNoContent {
		t.Fatalf("deleting the file = %d %s", code, body)
	}

	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var removed, erased, actor string
	eventually(t, "both halves of one removal in the audit trail", func() bool {
		err := owner.QueryRowContext(t.Context(),
			`SELECT coalesce(max(payload->>'sha256') FILTER (WHERE name = 'file.deleted'), ''),
			        coalesce(max(payload->>'sha256') FILTER (WHERE name = 'file.erased'), ''),
			        coalesce(max(actor::text)   FILTER (WHERE name = 'file.deleted'), '')
			   FROM audit_events WHERE payload->>'fileId' = $1`, id).
			Scan(&removed, &erased, &actor)
		return err == nil && removed == digest && erased == digest
	})
	t.Logf("audit trail: file.deleted sha256=%s actor=%s; file.erased sha256=%s", removed, actor, erased)
	if actor == "" {
		t.Errorf("the row the removal left names no actor (%s); the trail is meant to answer who, not only what",
			digest[:12])
	}

	// And the trail is not the only thing that changed: the subscription that
	// wrote the second record removed the bytes, which is what made the first
	// record's digest the last true sentence about them.
	var blobs int
	if err := filepath.WalkDir(cfg.Files.Dir, func(_ string, e os.DirEntry, err error) error {
		if err == nil && !e.IsDir() {
			blobs++
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", cfg.Files.Dir, err)
	}
	if blobs != 0 {
		t.Errorf("%d blobs are left in the installation's store after the trail says they were erased", blobs)
	}
}
