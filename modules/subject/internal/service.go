// This file is the whole of what modules/subject does: it holds no rows, so it
// owns the order, the shape and the two refusals.
//
// The order is module-name order, so one person's export does not depend on which
// module the resolver happened to build first, and one erasure reaches the same
// eight tables whichever way the composition lists them. The shape is the zip
// ADR 0020 §5 describes. The two refusals are the ones only the orchestrator can
// make: the address repeated wrongly, and the person whose row already carries the
// replacement this module writes. Everything else — what a task's description
// becomes, whether a page survives, who may not be erased while they are the last
// administrator — belongs to the owner of the row and is asked of it here, not
// decided from here.
package internal

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// service is the composition of the parts and nothing else: no store, no cache, no
// request rows. A composition with state of its own would be a second answer to
// "which requests exist", and the request rows are a door's business — the door
// this module has no opinion about yet.
type service struct {
	parts  []personaldata.Part
	people usercontracts.Service
	now    func() time.Time

	// bundles and notifier are the two ports the door needs and the two commands do not:
	// a bundle has to go somewhere and a person has to be told. A composition that wires
	// neither has the read and the erasure, and the door says so rather than storing a
	// bundle nowhere — which is why they are checked where they are used.
	bundles  contracts.BundleSource
	notifier contracts.Notifier
}

// Deps is what this module cannot make for itself.
type Deps struct {
	// Parts are the contributions every owner of a person's rows handed over.
	Parts []personaldata.Part
	// People is the one read across the boundary: the address and the name.
	People usercontracts.Service
	// Now is the clock the bundle's manifest, the receipt and the link's expiry are
	// stamped with; nil means db.Now. A command that reads its own watch is a command a
	// test cannot stop at one instant.
	Now func() time.Time
	// Bundles stores a bundle's bytes and holds them; Notes tells the person they are
	// there. Both belong to the door (contracts/requests.go) and nothing else.
	Bundles contracts.BundleSource
	Notes   contracts.Notifier
}

// NewService builds the module's commands over the contributions the composition
// collected. The value it returns also satisfies Walker, which is the one method the
// worker's job needs and no caller outside this module has.
func NewService(d Deps) contracts.Service {
	now := d.Now
	if now == nil {
		now = db.Now
	}
	people := d.People
	parts := d.Parts
	own := slices.Clone(parts)
	// Module-name order, once, at construction: the bundle and the receipt then
	// read the same whatever order the resolver built the contributors in.
	slices.SortFunc(own, func(a, b personaldata.Part) int {
		return strings.Compare(a.Module, b.Module)
	})
	seen := map[string]bool{}
	for _, p := range own {
		if p.Module == "" {
			panic("subject: a personal-data part with no module name is a part nobody can attribute")
		}
		if seen[p.Module] {
			panic("subject: two personal-data parts named " + p.Module + " would write one bundle entry and one of them would be missing")
		}
		seen[p.Module] = true
	}
	return &service{parts: own, people: people, now: now, bundles: d.Bundles, notifier: d.Notes}
}

// person reads the strings the contributors must stop finding, and is the reason
// this module needs the people service: only the orchestrator may look across the
// boundary, and only the person's own module holds the name and the address
// (ADR 0020 §1). A person with no row is nobody's data and answers ErrNotFound.
func (s *service) person(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (personaldata.Subject, error) {
	if s.people == nil {
		// Refused here rather than panicked at construction, because the manifest a reader
		// and a test build (`New`) composes nothing, and the same value has to answer both
		// questions. In a composed application the resolver refuses the module before this
		// line is reachable: it Needs the people service and would be built without it.
		return personaldata.Subject{}, errors.New("subject: no people service is composed, so an export and an erasure have nothing to read")
	}
	u, err := s.people.Get(ctx, tx, id)
	if err != nil {
		return personaldata.Subject{}, fmt.Errorf("subject: %w", err)
	}
	return personaldata.Subject{ID: u.ID, Email: u.Email, DisplayName: u.DisplayName, Handle: u.Handle}, nil
}

func (s *service) Export(ctx context.Context, tx db.Tx[db.Tenant], subject uuid.UUID) (*contracts.Bundle, error) {
	sj, err := s.person(ctx, tx, subject)
	if err != nil {
		return nil, err
	}
	at := s.now()

	bodies := make([]entry, 0, len(s.parts)+1)
	manifest := contracts.Manifest{Tenant: db.TenantOf(tx).ID, Subject: sj.ID, At: at}
	for _, p := range s.parts {
		// One failure aborts: a bundle that carries six owners and silently omits
		// two reads exactly like a person who has nothing in them.
		body, err := p.Data.Export(ctx, tx, sj)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		manifest.Parts = append(manifest.Parts, contracts.Part{
			Module: p.Module, Bytes: len(body), Digest: hex.EncodeToString(sum[:]),
		})
		bodies = append(bodies, entry{p.Module + ".json", body})
	}

	head, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("subject: marshal the bundle manifest: %w", err)
	}
	slices.SortFunc(bodies, func(a, b entry) int { return strings.Compare(a.name, b.name) })

	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	// One sorted write of every entry — the manifest's own name included, because
	// "entries are stored sorted by name" is the rule and a rule applied to all but
	// one entry is a convention somebody has to remember. A zero modified time is
	// what makes two exports of the same rows the same bytes; a bundle with a
	// timestamp in its entries could never be compared.
	for _, b := range append(bodies, entry{"manifest.json", head}) {
		if err := write(zw, b.name, b.body); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("subject: close the bundle: %w", err)
	}
	zipBytes := out.Bytes()
	sum := sha256.Sum256(zipBytes)
	return &contracts.Bundle{Zip: zipBytes, Digest: hex.EncodeToString(sum[:]), Manifest: manifest}, nil
}

// entry is one file of the archive, named and with its bytes already made.
type entry struct {
	name string
	body []byte
}

// write adds one stored entry. Store, and not Deflate: the zip must be the same
// bytes for the same rows on two machines and two Go versions, and a compression
// level is one more thing that could differ between them. The content is JSON a
// person will open in an editor, not a payload where the ratio is worth the risk.
func write(zw *zip.Writer, name string, body []byte) error {
	f, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		return fmt.Errorf("subject: open the bundle entry %s: %w", name, err)
	}
	if _, err := f.Write(body); err != nil {
		return fmt.Errorf("subject: write the bundle entry %s: %w", name, err)
	}
	return nil
}

func (s *service) Erase(ctx context.Context, tx db.Tx[db.Tenant], in contracts.EraseInput) (*contracts.Erasure, error) {
	sj, err := s.person(ctx, tx, in.Subject)
	if err != nil {
		return nil, err
	}
	at := s.now()

	// The second confirmation is asked before anything is written, and it is
	// checked against the row rather than against the caller: an address typed
	// from memory is a hint, and the row is what the tenant is being asked about.
	if !strings.EqualFold(strings.TrimSpace(in.Confirm), sj.Email) {
		return nil, fmt.Errorf("subject: erase %s: the address repeated is not the address this tenant holds: %w",
			in.Subject, crud.ErrInvalid)
	}

	// A row that already carries the replacement is an erasure that happened, and
	// the same request again answers the receipt rather than scrubbing the
	// anonymised strings a previous erasure left behind — which, rewritten a
	// second time, would rewrite nothing and pretend otherwise.
	if strings.EqualFold(sj.Email, personaldata.Subject{ID: sj.ID}.ErasedEmail()) {
		return &contracts.Erasure{Subject: sj.ID, Modules: s.names(), AlreadyErased: true, At: at}, nil
	}

	names := make([]string, 0, len(s.parts))
	for _, p := range s.parts {
		if err := p.Data.Erase(ctx, tx, sj); err != nil {
			// The caller's transaction decides what an owner's refusal costs; a
			// half-erased person is not a person who is erased, so this returns the
			// error and writes no event, and the command that owns the transaction
			// rolls back.
			return nil, err
		}
		names = append(names, p.Module)
	}

	requester, _ := tenancy.ActorFrom(ctx)
	// The event commits with the rows: the outbox write is in this transaction, so
	// a rolled-back erasure publishes nothing and a published erasure changed
	// something. This is also how the erasure reaches the trail, which subscribes
	// to every event the application emits.
	if err := events.Publish(ctx, tx, contracts.EventErased, contracts.Erased{
		Subject: sj.ID, Modules: names, Requester: requester, At: at,
	}); err != nil {
		return nil, err
	}
	return &contracts.Erasure{Subject: sj.ID, Modules: names, At: at}, nil
}

// names is the receipt's list for an erasure that ran nothing.
func (s *service) names() []string {
	names := make([]string, len(s.parts))
	for i, p := range s.parts {
		names[i] = p.Module
	}
	return names
}
