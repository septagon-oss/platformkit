// describe.go is the composition read back out of the kernel as data.
//
// Everything here is a projection of a value app.New already checked: the
// manifests, the operations the recording adapter kept, the resources kit/rest
// mounted, and the pool and transport the configuration selected. It adds no
// registry, no configuration namespace and no discovery — the description is
// assembled from the same objects that serve a request, on the same code path,
// which is the only way it cannot disagree with the application it describes.

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// DescribeVersion is the shape of a Description. It works the way
// screens.CatalogVersion does: it goes up when an existing key changes meaning,
// so a reader that cannot have understood the change refuses the document
// instead of misreading it. An added optional key does not raise it.
const DescribeVersion = 1

// kernelModule names the pseudo-module that owns what no module registered: the
// routes huma mounts for itself — /openapi.json, /openapi.yaml, /docs and the
// JSON Schema route. They are in the recording because Recorded is only worth
// reading if it is the whole list, and they are in the description for the same
// reason. The two probes and the static trees are not operations at all
// (httpx.API.Probes, httpx.API.Static) and so appear nowhere: a description of
// the operations cannot describe what deliberately is not one.
const kernelModule = "kernel"

// Description is the composed application: which half this process runs, how it
// moves events, how it holds its connections, and what each module in
// composition order declares.
//
// It is deterministic by construction. Modules keep composition order because
// that order is what migrates first and what navigation renders in; every other
// list is sorted. Nothing here comes from the environment — no timestamp, no
// hostname, no listen address, no DSN — so the same source produces the same
// bytes on every machine, which is what lets a test keep a committed description
// honest.
type Description struct {
	DescribeVersion int  `json:"describeVersion"`
	Role            Role `json:"role"`
	// Transport is the name the configuration and the role select: nats
	// .transport's two values. An application that injects Options.Transport
	// replaces the transport without naming one; this is the name the
	// configuration's own decision gives, which is what Run would use.
	Transport string              `json:"transport"`
	Pool      PoolDescription     `json:"pool"`
	Modules   []ModuleDescription `json:"modules"`
}

// PoolDescription is the connection policy databasePool resolved, in the units
// the configuration names them. A lifetime is a duration string because "30m"
// is what a deployment writes and 1800000000000 is not what it means.
type PoolDescription struct {
	MaxOpenConns    int    `json:"maxOpenConns"`
	MaxIdleConns    int    `json:"maxIdleConns"`
	ConnMaxLifetime string `json:"connMaxLifetime"`
}

// ModuleDescription is one manifest, as the kernel held it after Expand.
type ModuleDescription struct {
	Name          string                  `json:"name"`
	Permissions   []PermissionDescription `json:"permissions,omitempty"`
	Events        []string                `json:"events,omitempty"`
	Subscriptions []string                `json:"subscriptions,omitempty"`
	SubscribeAll  bool                    `json:"subscribeAll,omitempty"`
	Jobs          []JobDescription        `json:"jobs,omitempty"`
	Nav           []NavEntryDescription   `json:"nav,omitempty"`
	Migrations    []string                `json:"migrations,omitempty"`
	Adopts        []AdoptionDescription   `json:"adopts,omitempty"`
	Routes        []RouteDescription      `json:"routes,omitempty"`
	Resources     []ResourceDescription   `json:"resources,omitempty"`
}

// PermissionDescription is one key a role can be granted, and whose it is.
type PermissionDescription struct {
	Key      string `json:"key"`
	Operator bool   `json:"operator,omitempty"`
}

// JobDescription is one periodic job and its schedule. Exactly one of Cron and
// Every is set, which is jobs.Valid's rule as well; Every is a duration string
// so that a job scheduled every second does not read as 1000000000.
type JobDescription struct {
	Name     string `json:"name"`
	Cron     string `json:"cron,omitempty"`
	Every    string `json:"every,omitempty"`
	Parallel bool   `json:"parallel,omitempty"`
}

// NavEntryDescription is one link the module contributes to navigation. The
// order is the manifest's rather than alphabetical because kit/module
// deliberately has no Order field — navigation renders in composition order —
// and a description that sorted these would claim an order the application does
// not have.
type NavEntryDescription struct {
	Label      string `json:"label"`
	Path       string `json:"path"`
	Permission string `json:"permission,omitempty"`
}

// AdoptionDescription is migration history this module took over from another
// owner, as db.Adoption declares it.
type AdoptionDescription struct {
	Owner    string  `json:"owner"`
	Versions []int64 `json:"versions"`
}

// RouteDescription is one operation, with the authorization it declares and the
// events it says it publishes.
type RouteDescription struct {
	OperationID string          `json:"operationId"`
	Method      string          `json:"method"`
	Path        string          `json:"path"`
	Summary     string          `json:"summary,omitempty"`
	Auth        json.RawMessage `json:"auth,omitempty"`
	Events      []string        `json:"events,omitempty"`
}

// ResourceDescription is one entity kit/rest mounted, as the generated screens
// see it: the same two permissions the routes carry, the fields a command owns,
// and whether the tenant has one of these or many. It does not repeat the field
// list — /api/v1/admin/resources serves that schema from the same
// httpx.Resources this reads, and a second copy of every entity in the
// description would be a second thing to keep in step.
type ResourceDescription struct {
	Entity        string               `json:"entity"`
	Path          string               `json:"path"`
	Read          string               `json:"read"`
	Write         string               `json:"write"`
	OperatorRead  bool                 `json:"operatorRead,omitempty"`
	OperatorWrite bool                 `json:"operatorWrite,omitempty"`
	Immutable     []string             `json:"immutable,omitempty"`
	Singleton     bool                 `json:"singleton,omitempty"`
	Commands      []CommandDescription `json:"commands,omitempty"`
}

// CommandDescription is one lifecycle route beyond the five, and who may call
// it. Fields is the shape of its argument, in the order the request body
// declares it — the same order the catalog gives a generated form.
type CommandDescription struct {
	Verb       string             `json:"verb"`
	Summary    string             `json:"summary,omitempty"`
	Collection bool               `json:"collection,omitempty"`
	Auth       json.RawMessage    `json:"auth,omitempty"`
	Fields     []FieldDescription `json:"fields,omitempty"`
}

// FieldDescription is one field of a command's argument, by name and type.
type FieldDescription struct {
	Name string           `json:"name"`
	Type entity.FieldType `json:"type"`
}

// Describe returns the composition as data. It builds the API exactly as a boot
// does — every module registers its routes, and every boot gate runs — and then
// reads back what the kernel recorded, so no description can be produced for an
// application that would not start.
//
// It serves nothing, migrates nothing, relays no event and schedules no job: it
// stops earlier than Start, before a transport is opened. conn is needed for one
// reason — httpx.New refuses an API with no connection, because every tenant
// request opens its transaction on one — so a caller that only wants the
// description still has to have opened it. Pass the application connection Run
// would have used; Describe issues no query against it.
func (a *App) Describe(ctx context.Context, conn *db.Conn) (Description, error) {
	if conn == nil {
		return Description{}, errors.New("app: Describe needs the application connection; httpx.New requires one and does not accept nil")
	}
	built, err := a.buildAPI(ctx, conn)
	if err != nil {
		return Description{}, err
	}
	broker, err := useJetStream(a.cfg.NATS.Transport, a.opts.Role)
	if err != nil {
		return Description{}, err
	}
	pool := databasePool(a.cfg.Database)
	transport := "memory"
	if broker {
		transport = "jetstream"
	}

	routes, orphanRoutes, err := routesByOwner(built.api.Recorded(), a.mods)
	if err != nil {
		return Description{}, err
	}
	resources, orphanResources, err := resourcesByOwner(built.api.Resources(), a.mods)
	if err != nil {
		return Description{}, err
	}

	out := Description{
		DescribeVersion: DescribeVersion,
		Role:            a.opts.Role,
		Transport:       transport,
		Pool:            PoolDescription{pool.MaxOpenConns, pool.MaxIdleConns, pool.ConnMaxLifetime.String()},
	}
	for _, m := range a.mods {
		d, err := a.describeModule(m, routes[m.Name], resources[m.Name])
		if err != nil {
			return Description{}, err
		}
		out.Modules = append(out.Modules, d)
	}
	// What no module owns is the kernel's, and is said out loud rather than left
	// out: a description missing the documentation routes would read as an
	// application that serves none.
	if len(orphanRoutes) > 0 || len(orphanResources) > 0 {
		out.Modules = append(out.Modules, ModuleDescription{
			Name: kernelModule, Routes: orphanRoutes, Resources: orphanResources,
		})
	}
	return out, nil
}

// describeModule is one manifest's entry. Its lists are sorted here, at the
// point they leave the typed value they came from, so nothing upstream has to be
// kept in order for the bytes to be stable.
func (a *App) describeModule(m module.Module, routes []RouteDescription, resources []ResourceDescription) (ModuleDescription, error) {
	out := ModuleDescription{Name: m.Name, Routes: routes, Resources: resources}
	for _, p := range m.Permissions {
		out.Permissions = append(out.Permissions, PermissionDescription{Key: p.Key, Operator: p.Operator})
	}
	sort.Slice(out.Permissions, func(i, j int) bool { return out.Permissions[i].Key < out.Permissions[j].Key })

	out.Events = append(out.Events, m.Events...)
	sort.Strings(out.Events)

	for _, s := range m.Subscriptions {
		out.Subscriptions = append(out.Subscriptions, s.Name)
	}
	sort.Strings(out.Subscriptions)
	// The flag is the fact the manifest declared, not something inferred from a
	// subscription list that happens to be complete: Expand cleared it on the
	// manifest Describe is reading.
	out.SubscribeAll = a.subscribeAll[m.Name]

	for _, j := range m.Jobs {
		job := JobDescription{Name: j.Name, Cron: j.Cron, Parallel: j.Parallel}
		if j.Every > 0 {
			// A cron job's zero interval is not a job that runs every zero
			// seconds; jobs.Valid refuses both being set, and the description
			// says the one that is.
			job.Every = j.Every.String()
		}
		out.Jobs = append(out.Jobs, job)
	}
	sort.Slice(out.Jobs, func(i, j int) bool { return out.Jobs[i].Name < out.Jobs[j].Name })

	for _, n := range m.Nav {
		out.Nav = append(out.Nav, NavEntryDescription{Label: n.Label, Path: n.Path, Permission: n.Permission})
	}

	files, err := migrationFiles(m.Migrations)
	if err != nil {
		return out, fmt.Errorf("app: describe module %q: %w", m.Name, err)
	}
	out.Migrations = files

	for _, ad := range m.Adopts {
		out.Adopts = append(out.Adopts, AdoptionDescription{Owner: ad.Owner, Versions: append([]int64(nil), ad.Versions...)})
	}
	sort.Slice(out.Adopts, func(i, j int) bool { return out.Adopts[i].Owner < out.Adopts[j].Owner })
	for i := range out.Adopts {
		sort.Slice(out.Adopts[i].Versions, func(x, y int) bool { return out.Adopts[i].Versions[x] < out.Adopts[i].Versions[y] })
	}
	return out, nil
}

// migrationFiles are the SQL files this owner's filesystem carries, in name
// order. Directories and anything that is not SQL are skipped, as kit/db skips
// them: the history is the list of names, not their bytes.
func migrationFiles(fsys fs.FS) ([]string, error) {
	if fsys == nil {
		return nil, nil
	}
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

// authJSON renders a declaration with httpx.Auth's own encoder, so the guard a
// reviewer reads in a description is byte for byte the one /openapi.json
// publishes and the request middleware enforces. It returns the error it is
// given rather than swallowing it: the encoder writes three string fields and
// cannot fail today, and a declaration that ever grows a field that can must
// fail the description instead of quietly dropping a guard from it.
func authJSON(a httpx.Auth) (json.RawMessage, error) {
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, fmt.Errorf("app: describe: the authorization declaration did not encode: %w", err)
	}
	return raw, nil
}

// routesByOwner groups every recorded operation under the module it belongs to,
// and returns the ones no module owns. Three signals, in order of how much they
// can be trusted: see ownerOf.
func routesByOwner(ops []*huma.Operation, mods []module.Module) (map[string][]RouteDescription, []RouteDescription, error) {
	by := map[string][]RouteDescription{}
	var orphans []RouteDescription
	for _, op := range ops {
		r := RouteDescription{OperationID: op.OperationID, Method: op.Method, Path: op.Path, Summary: op.Summary}
		// Only a declaration httpx minted counts; an operation carrying nothing
		// under the key has no authorization to report, which is what
		// ValidateDeclarations already refused if it reached here.
		if auth, ok := op.Extensions[httpx.AuthExtension].(httpx.Auth); ok && auth.Declared() {
			raw, err := authJSON(auth)
			if err != nil {
				return nil, nil, err
			}
			r.Auth = raw
		}
		if names, ok := op.Extensions[httpx.EventsExtension].([]string); ok {
			r.Events = append(r.Events, names...)
			sort.Strings(r.Events)
		}
		if owner := ownerOf(op, mods); owner != "" {
			by[owner] = append(by[owner], r)
			continue
		}
		orphans = append(orphans, r)
	}
	for _, list := range by {
		sortRoutes(list)
	}
	sortRoutes(orphans)
	return by, orphans, nil
}

func sortRoutes(list []RouteDescription) {
	sort.Slice(list, func(i, j int) bool {
		if list[i].Path != list[j].Path {
			return list[i].Path < list[j].Path
		}
		return list[i].Method < list[j].Method
	})
}

// ownerOf answers which module registered an operation, or "" when none did.
//
// The path is the strongest claim, because it is the one the module chose for
// itself: an API lives at /api/v1/<module>/... and a module that serves pages
// of its own lives under /<module>/. Failing that, an event the operation
// publishes is namespaced by the module that emits it, and an operation id
// begins with that module's name — which is what reaches a module whose paths
// carry somebody else's prefix, and nothing else does.
func ownerOf(op *huma.Operation, mods []module.Module) string {
	for _, m := range mods {
		for _, at := range []string{"/api/v1/" + m.Name, "/" + m.Name} {
			if op.Path == at || strings.HasPrefix(op.Path, at+"/") {
				return m.Name
			}
		}
	}
	published, _ := op.Extensions[httpx.EventsExtension].([]string)
	for _, m := range mods {
		for _, e := range published {
			if strings.HasPrefix(e, m.Name+".") {
				return m.Name
			}
		}
	}
	for _, m := range mods {
		if strings.HasPrefix(op.OperationID, m.Name+"-") {
			return m.Name
		}
	}
	return ""
}

// resourcesByOwner groups the mounted resources by the module their Spec named,
// and keeps any that named a module this composition does not have. That cannot
// happen for a composition New accepted — the Spec's module is the string the
// manifest carries — and the alternative to checking is a resource that silently
// appears in no entry of the description.
func resourcesByOwner(resources []httpx.Resource, mods []module.Module) (map[string][]ResourceDescription, []ResourceDescription, error) {
	by := map[string][]ResourceDescription{}
	var orphans []ResourceDescription
	for _, r := range resources {
		out := ResourceDescription{
			Entity: r.Entity, Path: r.Path, Read: r.Read, Write: r.Write,
			OperatorRead: r.OperatorRead, OperatorWrite: r.OperatorWrite,
			Immutable: append([]string(nil), r.Immutable...), Singleton: r.Singleton,
		}
		sort.Strings(out.Immutable)
		for _, c := range r.Commands {
			cmd := CommandDescription{Verb: c.Verb, Summary: c.Summary, Collection: c.Collection}
			if c.Auth.Declared() {
				raw, err := authJSON(c.Auth)
				if err != nil {
					return nil, nil, err
				}
				cmd.Auth = raw
			}
			for _, f := range c.Fields {
				cmd.Fields = append(cmd.Fields, FieldDescription{Name: f.Name, Type: f.Type})
			}
			out.Commands = append(out.Commands, cmd)
		}
		sort.Slice(out.Commands, func(i, j int) bool { return out.Commands[i].Verb < out.Commands[j].Verb })
		if moduleNamed(mods, r.Module) {
			by[r.Module] = append(by[r.Module], out)
			continue
		}
		orphans = append(orphans, out)
	}
	for _, list := range by {
		sort.Slice(list, func(i, j int) bool { return list[i].Entity < list[j].Entity })
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].Entity < orphans[j].Entity })
	return by, orphans, nil
}

func moduleNamed(mods []module.Module, name string) bool {
	for _, m := range mods {
		if m.Name == name {
			return true
		}
	}
	return false
}
