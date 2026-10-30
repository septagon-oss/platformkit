package pkit

import (
	"errors"
	"reflect"
)

// schemaV1 names the format Description is written in. It is a field of every
// document rather than an exported constant, because a Go consumer reads the
// struct and a name nothing imports is a public promise; ui/export's
// "platformkit.design-export.v1" is the same shape for the same reason. A v2
// would be a new string and a new set of keys, never a quiet addition to this
// one — which is why there is no options bag on Describe asking for more.
const schemaV1 = "platformkit.composition.v1"

// Description is the composition the resolver settled for one deployment: the
// modules that are built, in the order they are built, who supplies whose need,
// who contributes what to whom, what Choose settled over whom, which
// implementation FromDeployment picked, which configuration sections were read,
// and the roles the app recorded.
//
// It is the resolved declarations and nothing else: routes, the kernel's three
// ports, tenant hosts and any tenant data, the built manifests, observed runtime
// behaviour and a timestamp are each absent because this package does not own
// them — pkit/README.md names the six and why.
//
// The one rule about a refusal: when the composition does not resolve, the
// document carries schema, app, environment, resolved:false and problems, and no
// module, contract, provider, implementation or role.
type Description struct {
	Schema      string             `json:"schema"`
	App         string             `json:"app"`
	Environment Environment        `json:"environment"`
	Resolved    bool               `json:"resolved"`
	Modules     []DescribedModule  `json:"modules"`
	Choices     []DescribedChoice  `json:"choices,omitempty"`
	Roles       []DescribedRole    `json:"roles,omitempty"`
	Problems    []DescribedProblem `json:"problems"`
	// Suppressed is present only on a refusal: the modules whose build did not
	// run because a module they wait on refused, in build order.
	Suppressed []DescribedSuppression `json:"suppressed,omitempty"`
	// Cloud is the deployment's answer about its seams: which object store, cache,
	// transport, mail sender, policy engine, telemetry exporter, identity door and
	// flag source the resolved configuration runs. It is absent on a refusal with
	// the rest of the composition, because a deployment that does not resolve has no
	// answer about what runs. It names key names and never a value: an address is the
	// deployment's and is nowhere in this document.
	Cloud *DescribedCloud `json:"cloud,omitempty"`
}

// DescribedCloud is one deployment read against the cloud profile it named.
// Profile is the resolved name — an empty setting resolves to local and reads as
// local — and Seams is one entry per seam in the order Explain prints.
type DescribedCloud struct {
	Profile     string          `json:"profile"`
	Environment Environment     `json:"environment"`
	Seams       []DescribedSeam `json:"seams"`
}

// DescribedSeam is one infrastructure question and the answer the configuration
// gives it. Provider is the name this kernel prints for the thing that runs;
// Source says where the setting behind it came from — cloud profile, deployment,
// default or kernel; Keys names the settings the profile wrote for this seam, and
// writes no value; Served is false for the two seams no profile moves and for any
// seam the chosen profile declines to name, which Note then explains.
type DescribedSeam struct {
	Seam     Seam     `json:"seam"`
	Provider string   `json:"provider"`
	Source   string   `json:"source"`
	Keys     []string `json:"keys,omitempty"`
	Served   bool     `json:"served"`
	Note     string   `json:"note,omitempty"`
}

// DescribedModule is one module as the resolution placed it. Every array is in
// the order the module's own source declares it, which is the order Explain
// prints, so a reader can find the line in the file. Module names in `after`,
// `from` and `to` are the `name` of a module in this same document.
type DescribedModule struct {
	Name string `json:"name"`

	// Provides is every contract this module says it is the one of.
	Provides []string `json:"provides,omitempty"`

	// Needs is one contract per Needs[T]; Uses one per Optional[T]; Takes one
	// per Needs[[]E] or Optional[[]E]. A `from` that is "" says the app composes
	// no provider, which is only ever possible for an Optional need, and a `from`
	// that is [] says nobody contributes one today — zero or more is a real
	// state (decision 0074), so "nobody" is answered, not left absent.
	Needs []DescribedNeed  `json:"needs,omitempty"`
	Uses  []DescribedNeed  `json:"uses,omitempty"`
	Takes []DescribedTaker `json:"takes,omitempty"`

	// Contributes is one per Contributes[T], with the modules that take it. No
	// composed taker is a note, not a refusal, and reads as an empty `to`.
	Contributes []DescribedContribution `json:"contributes,omitempty"`

	// After is the modules this one is built after — the build order's own
	// answer, not the module's declaration. RunsLast is the module that
	// After(Everything) names, which is the only module whose own sentence is
	// about a phase rather than a contract.
	After    []string `json:"after,omitempty"`
	Reads    []string `json:"reads,omitempty"`
	RunsLast bool     `json:"runsLast,omitempty"`

	// Implementation is the one the deployment picked, with the *names* of the
	// inputs that pick reads. Their values are the deployment's and are nowhere
	// in this document.
	Implementation *DescribedImplementation `json:"implementation,omitempty"`
}

// DescribedNeed is one contract and who supplies it.
type DescribedNeed struct {
	Contract string `json:"contract"`
	From     string `json:"from"`
}

// DescribedTaker is one contract taken as a collection and every module that
// contributes one.
type DescribedTaker struct {
	Contract string   `json:"contract"`
	From     []string `json:"from"`
}

// DescribedContribution is one contributed contract and every module that
// takes it.
type DescribedContribution struct {
	Contract string   `json:"contract"`
	To       []string `json:"to"`
}

// DescribedImplementation is the implementation FromDeployment settled on.
type DescribedImplementation struct {
	Name      string   `json:"name"`
	Inputs    []string `json:"inputs,omitempty"`
	Simulated bool     `json:"simulated,omitempty"`
}

// DescribedChoice is what Choose settled: the contract, the module picked, and
// the ones it was picked over.
type DescribedChoice struct {
	Contract   string   `json:"contract"`
	Picked     string   `json:"picked"`
	PassedOver []string `json:"passedOver,omitempty"`
}

// DescribedRole is one role the app recorded. It is a projection of Role, not
// Role itself: that type is a declaration a composition writes, and giving it
// wire keys would change what an existing public type encodes to for a reason
// that belongs to this document alone.
type DescribedRole struct {
	Name   string   `json:"name"`
	Grants []string `json:"grants,omitempty"`
}

// DescribedProblem is one refusal. Cause is the method that caused it — Use,
// Choose, Deployment or Build — and Sentence is exactly the words Validate
// returns with the "pkit: <app>: <cause>: " prefix, minus the prefix, so a
// caller can print what the resolver said rather than re-word it.
type DescribedProblem struct {
	Cause    string `json:"cause"`
	Sentence string `json:"sentence"`
	// Module is the module whose build refused; "" when the resolver, the app's
	// name or the manifest gates refused.
	Module string `json:"module,omitempty"`
	// Contract and Requiredness are set when the refusal is a read the plan could
	// not answer: the contract as Explain prints it, and "required" (declared with
	// Needs) or "undeclared" (declared with neither Needs nor Optional). An
	// Optional read never fails, so "optional" is never written.
	Contract     string `json:"contract,omitempty"`
	Requiredness string `json:"requiredness,omitempty"`
}

// Describe reads the composition the resolver settled, as values, for one
// deployment: build order, suppliers, contributions, what Choose settled, the
// implementation the deployment picked, the configuration sections each module
// read (names only) and the recorded roles — and, when the composition does not
// resolve, the problems that are why, with no graph beside them. The value is
// non-nil on both paths: on refusal it is the machine-readable form of it.
//
// Every module's own build function runs, in build order, because what a module
// reads and puts is observable only from inside its own build. Nothing in this
// phase opens a pool, runs a migration, dials a broker, listens on a port or
// starts a server, and a Deployment with no transports and no caches is described
// as readily as a full one. A build function is the module's own code and this
// package cannot bound it: see pkit/README.md.
func (a *App) Describe(d Deployment) (*Description, error) {
	p, _, _, err := a.compose(d)
	if err != nil {
		// The problems are the error's own fields, not a second reading of them:
		// compose answers every refusal as a CompositionError.
		doc := &Description{
			Schema:      schemaV1,
			App:         a.name,
			Environment: d.Environment,
			Modules:     []DescribedModule{},
			Problems:    []DescribedProblem{},
		}
		var ce *CompositionError
		if errors.As(err, &ce) {
			doc.Problems, doc.Suppressed = ce.Problems, ce.Suppressed
		}
		return doc, err
	}
	return a.described(p, d.Environment), nil
}

// described is the one projection of a resolved plan. Explain renders it as
// sentences and Describe hands it over as values; neither reads the plan's maps
// for itself, which is what makes the text and the document answer the same
// composition and makes the bytes depend on nothing but the composition.
func (a *App) described(p *plan, env Environment) *Description {
	return &Description{
		Schema:      schemaV1,
		App:         a.name,
		Environment: env,
		Resolved:    true,
		Modules:     a.describedModules(p),
		Choices:     describedChoices(p.choices),
		Roles:       describedRoles(a.roles),
		Problems:    []DescribedProblem{},
		Cloud:       describedCloud(p.cloud, env),
	}
}

// describedCloud projects the seams resolveCloud settled. The environment travels
// with them because the sentence about a profile is incomplete without it, and the
// renderer reads the projection rather than the plan — which is the one reason the
// text and this document cannot answer about different deployments.
func describedCloud(c cloudPlan, env Environment) *DescribedCloud {
	out := &DescribedCloud{Profile: c.profile, Environment: env, Seams: make([]DescribedSeam, 0, len(c.seams))}
	for _, s := range c.seams {
		out.Seams = append(out.Seams, DescribedSeam{
			Seam: s.seam, Provider: s.provider, Source: s.source,
			Keys: s.keys, Served: s.served, Note: s.note,
		})
	}
	return out
}

// describedModules walks the build order, which is the order the kernel builds
// and mounts in. Nothing here ranges a plan map: each array comes from the order
// above, from the module's own declarations, or from a list the resolver already
// appended, so an equivalent composition encodes to the same bytes every time.
func (a *App) describedModules(p *plan) []DescribedModule {
	out := make([]DescribedModule, 0, len(p.order))
	for _, m := range p.order {
		dm := DescribedModule{Name: m.name, RunsLast: m == p.last}
		for _, dl := range m.decls {
			// The key is printed only for the kinds that carry a contract: a phase
			// or a self-description names none, and contract of no type panics.
			switch dl.kind {
			case provides:
				dm.Provides = append(dm.Provides, contract(dl.key))
			case needs, optional:
				key := contract(dl.key)
				switch {
				case dl.many:
					dm.Takes = append(dm.Takes, DescribedTaker{Contract: key, From: describedNames(p.contributors[dl.key])})
				case dl.kind == needs:
					dm.Needs = append(dm.Needs, DescribedNeed{Contract: key, From: describedName(p.from[edge{m, dl.key}])})
				default:
					dm.Uses = append(dm.Uses, DescribedNeed{Contract: key, From: describedName(p.from[edge{m, dl.key}])})
				}
			case contributes:
				dm.Contributes = append(dm.Contributes, DescribedContribution{Contract: contract(dl.key), To: describedNames(p.takers[dl.key])})
			case fromDeployment:
				dm.Implementation = describedImplementation(p.impl[m], dl.impls)
			}
		}
		dm.After = describedNames(p.deps[m])
		dm.Reads = describedReads(p.reads[m])
		out = append(out, dm)
	}
	return out
}

func describedChoices(cs []choice) []DescribedChoice {
	if len(cs) == 0 {
		return nil
	}
	out := make([]DescribedChoice, 0, len(cs))
	for _, c := range cs {
		out = append(out, DescribedChoice{Contract: contract(c.key), Picked: c.picked.name, PassedOver: c.passed})
	}
	return out
}

func describedRoles(rs []Role) []DescribedRole {
	if len(rs) == 0 {
		return nil
	}
	out := make([]DescribedRole, 0, len(rs))
	for _, r := range rs {
		out = append(out, DescribedRole{Name: r.Name, Grants: r.Grants})
	}
	return out
}

func describedProblems(issues []issue) []DescribedProblem {
	out := make([]DescribedProblem, 0, len(issues))
	for _, in := range issues {
		out = append(out, DescribedProblem{
			Cause: in.method, Sentence: in.err.Error(),
			Module: in.module, Contract: in.contract, Requiredness: in.requiredness,
		})
	}
	return out
}

func describedImplementation(name string, impls []Implementation) *DescribedImplementation {
	if name == "" {
		return nil
	}
	di := &DescribedImplementation{Name: name}
	for _, impl := range impls {
		if impl.Name != name {
			continue
		}
		di.Inputs = append(di.Inputs, impl.Inputs...)
		di.Simulated = impl.Simulated
	}
	return di
}

// describedNames names modules for the document: bare, as the module appears in
// `modules[].name`, and never nil — an empty list is the answer "nobody", and an
// absent key would read as "not asked".
func describedNames(ms []*Module) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.name)
	}
	return out
}

func describedName(m *Module) string {
	if m == nil {
		return ""
	}
	return m.name
}

// describedReads names the configuration sections a module asked for, in the
// order it asked, once each: the section is the fact, and a module that asked
// twice asked for one thing twice. Names only — plan.cfg is the deployment's and
// no value read through Config is reachable from here.
func describedReads(ts []reflect.Type) []string {
	if len(ts) == 0 {
		return nil
	}
	out := make([]string, 0, len(ts))
	seen := map[reflect.Type]bool{}
	for _, t := range ts {
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, contract(t))
	}
	return out
}

// suffixed spells module names the way the composition file spells them, for the
// half of the text that is prose about names.
func suffixed(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = n + ".Module"
	}
	return out
}
