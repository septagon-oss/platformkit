package pkit

// The seams: the eight infrastructure questions a deployment answers, read off
// the configuration it resolved and printed one per seam.
//
// A seam is not a module and not a port. It is the question a person asks when
// they move an installation somewhere — where do the bytes go, who holds the
// shared values, how does an event travel — and the answer is always one of a
// closed set of names this file owns. Nothing here decides which answer is right:
// kit/config holds the profile table, and this file reads the resolved settings
// back, which is why the answer cannot disagree with the wiring.
//
// Two rules hold throughout: nothing is dialed, because construction is not
// readiness, and no value of the deployment's is ever printed — sentences and the
// document name seams, providers and key names, never the address behind a key.

import (
	"errors"
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/config"
)

// Seam names one infrastructure question a deployment answers.
type Seam string

// The eight seams, in the one order Explain prints them and Describe arrays
// them: the storage the bytes go to, the values every replica shares, the way an
// event travels, how a notice is sent, who decides a permission, where the
// measurements go, who a person proves they are to, and what decides an optional
// behaviour. The last two of the eight sit in the middle of the list rather than
// at the end because the order is the deployment's reading order — the things it
// has to provide the money for first — and not a grouping by kind.
const (
	Storage   Seam = "storage"
	Cache     Seam = "cache"
	Events    Seam = "events"
	Mail      Seam = "mail"
	Policy    Seam = "policy"
	Telemetry Seam = "telemetry"
	Identity  Seam = "identity"
	Flags     Seam = "flags"
)

// The words that say where a seam's answer came from. Exactly one is printed per
// seam, and the difference between the first two is the whole of what a profile
// does: it writes what nobody wrote, and an operator who wrote it themselves keeps
// what they wrote.
const (
	sourceProfile    = "cloud profile"
	sourceDeployment = "deployment"
	sourceDefault    = "default"
	sourceKernel     = "kernel"
)

// notACloudSeam is the note policy and flags carry. Both are real seams — a
// deployment does answer those two questions — and neither has a setting a profile
// could move: the policy engine is in-process Rego built from a file the product
// embeds, and a feature flag is configuration by decision. Marking them is what
// keeps "every seam prints a provider" true without refusing a boot for a reason
// no configuration can correct.
const notACloudSeam = "not a cloud seam"

// azureNamesNothing is the seam note for storage under a profile that declines to
// name an object store, and azureStorageSentence is the refusal beside it. Azure
// Blob speaks its own REST API and the only object-store client this build links
// speaks S3, so the profile writes no key rather than pointing the deployment at
// the disk it may not have. The sentence names the three keys to write instead,
// because the fix is not "pick another profile" for a deployment that is already
// there: it is to name the store, or to link a client, which is a dependency
// decision and not a configuration one.
const azureNamesNothing = "cloud profile azure names no provider here"

const azureStorageSentence = "cloud profile azure names no storage provider: Azure Blob speaks no S3 API and this build links no Blob client; " +
	"set " + config.StoreS3Key + ", " + config.S3EndpointKey + " and " + config.S3BucketKey + " yourself, or choose local, self-hosted, aws or gcp"

// s3NotWired is the sentence for the keys a deployment wrote for a store the
// composition was never given. It is one sentence per deployment and not one per
// key: the fix is one line, and four sentences about it is four chances to fix the
// wrong one. The keys are named in the order the surface lists them.
func s3NotWired(keys []string) string {
	return fmt.Sprintf("%s names an S3 store the composition does not wire; set %s to %s",
		strings.Join(keys, ", "), config.StoreS3Key, config.StoreS3)
}

// seamNeeds is the sentence for a setting the profile asked for and the
// deployment did not provide. It is pickImplementation's shape — a thing the
// deployment needs in a named environment, with what to do about it beside it —
// kept as one grammar rather than a second one: the resolver should not say
// "needs X in production" two ways because one subject is a module and the other
// is a seam.
func seamNeeds(s Seam, key string, env Environment) string {
	return fmt.Sprintf("%s needs %s in %s: set it, or the deployment cannot serve %s there", s, key, env, s)
}

// seamDef is one seam: how to say what runs, and how to tell whether the
// deployment wrote the setting that answer turns on.
type seamDef struct {
	seam Seam
	// provider names what runs, read off the resolved configuration. The names are
	// this package's own vocabulary, so two spellings of one adapter — s3 versus
	// S3, valkey versus Redis — cannot reach a reader.
	provider func(config.Config) string
	// decided reports whether the deployment wrote the setting the answer turns
	// on. A seam that turns on an absent setting answers "default": the surface
	// decided it, which is a fact about the deployment and not a gap in a profile.
	decided func(config.Config) bool
	// note marks the seams no profile moves.
	note string
}

// seams is the whole vocabulary, in printed order.
var seams = []seamDef{
	{
		seam: Storage,
		provider: func(c config.Config) string {
			if c.Files.Store == config.StoreS3 {
				return "s3"
			}
			return config.StoreLocalDisk
		},
		decided: func(c config.Config) bool { return c.Files.Store != "" },
	},
	{
		seam: Cache,
		provider: func(c config.Config) string {
			if c.Cache.Adapter == "valkey" {
				return "valkey"
			}
			return "in-process"
		},
		decided: func(c config.Config) bool { return c.Cache.Adapter != "" },
	},
	{
		seam: Events,
		// by-role is the honest answer to "which transport": kit/app chooses memory
		// or JetStream from the role the process runs under and no setting moves it.
		// A deployment that wrote nats.transport gets its own word back.
		provider: func(c config.Config) string {
			if c.NATS.Transport != "" {
				return c.NATS.Transport
			}
			return "by-role"
		},
		decided: func(c config.Config) bool { return c.NATS.Transport != "" },
	},
	{
		// smtp is what FromDeployment picks from the same three keys, so the seam
		// line and the composition file cannot disagree about the mail carrier: both
		// read mail.host, one through this file and one through the deployment's
		// inputs, and the inputs are filled from that setting by the process.
		seam:     Mail,
		provider: func(c config.Config) string { return ternary(c.Mail.Enabled(), "smtp", "mailbox") },
		decided:  func(c config.Config) bool { return c.Mail.Enabled() },
	},
	{
		seam:     Policy,
		provider: func(config.Config) string { return "rego-in-process" },
		decided:  func(config.Config) bool { return false },
		note:     notACloudSeam,
	},
	{
		// An empty endpoint exports nothing, by decision and not by default: the
		// process still propagates a trace it was handed, it just keeps no spans of
		// its own. "none" says that rather than printing an empty provider.
		seam:     Telemetry,
		provider: func(c config.Config) string { return ternary(c.Telemetry.OTLPEndpoint != "", "otlp", "none") },
		decided:  func(c config.Config) bool { return c.Telemetry.OTLPEndpoint != "" },
	},
	{
		// An issuer either exists or the password door is the whole door. Neither is
		// a deployment that forgot something, which is why no profile demands either
		// half of it.
		seam:     Identity,
		provider: func(c config.Config) string { return ternary(c.Auth.OIDC.Enabled(), "oidc", "password-only") },
		decided:  func(c config.Config) bool { return c.Auth.OIDC.Enabled() },
	},
	{
		seam:     Flags,
		provider: func(config.Config) string { return "configuration" },
		decided:  func(config.Config) bool { return false },
		note:     notACloudSeam,
	},
}

func ternary(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

// seamOfKey says which seam a setting belongs to. It is what lets the profile
// table in kit/config stay a table of settings while the sentences stay sentences
// about seams: the demand for a key comes from that table, the seam that owes the
// deployment an answer about it comes from here, and one test walks the two so
// neither can name a key the other has never heard of.
var seamOfKey = map[string]Seam{
	config.StoreS3Key:         Storage,
	config.S3EndpointKey:      Storage,
	config.S3BucketKey:        Storage,
	config.S3RegionKey:        Storage,
	config.S3SchemeKey:        Storage,
	config.S3AccessKeyKey:     Storage,
	config.S3SecretKeyKey:     Storage,
	config.CacheAdapterKey:    Cache,
	config.CacheURLKey:        Cache,
	config.CacheAppKey:        Cache,
	config.NATSTransportKey:   Events,
	"mail.host":               Mail,
	"mail.from":               Mail,
	"telemetry.otlp_endpoint": Telemetry,
	"auth.oidc.issuer":        Identity,
}

// resolvedKey reads the value of the settings a profile writes. The set is small
// and closed — one adapter name per seam — so it is named key by key rather than
// reached for by reflection: a profile that learned to write a value this function
// does not know would be a profile writing something nothing can print back.
func resolvedKey(name string, c config.Config) (string, bool) {
	switch name {
	case config.StoreS3Key:
		return c.Files.Store, true
	case config.CacheAdapterKey:
		return c.Cache.Adapter, true
	case config.NATSTransportKey:
		return c.NATS.Transport, true
	}
	return "", false
}

// cloudPlan is what one deployment's configuration answers about its seams: the
// profile it resolved to, one state per seam, and the sentences a boot should say
// out loud. It is computed inside resolve, which is the one place that collects
// every problem a deployment has, and projected by described rather than rendered
// twice.
type cloudPlan struct {
	profile string
	seams   []seamState
	// warnings are the sentences for a deployment that is not refused — because it
	// runs in development, or because the profile lost to a value the operator wrote
	// first — and should still hear what it is missing. They are logged at Build and
	// appear nowhere else: Explain is a reading, and a reading that logs every time a
	// client asks it would make the composition file depend on a log sink.
	warnings []string
}

// seamState is one seam's answer: who runs, where that came from, which keys the
// profile wrote to get there, whether a profile serves the seam at all, and the
// note that explains one that does not. Every field is printed — by Explain, by
// Describe or by the boot log — because a fact about the deployment that nothing
// reads is a fact nobody checked.
type seamState struct {
	seam     Seam
	provider string
	source   string
	keys     []string
	served   bool
	note     string
}

// resolveCloud reads a deployment's configuration against the cloud profile it
// named.
//
// The seams are answered in every environment, because what runs is a fact about
// the configuration and not about who is watching. What the environment decides is
// severity: staging and production refuse a profile whose inputs the deployment
// never named, and development warns — a laptop halfway through a setup is the
// normal state of a laptop, not a broken deployment. A warning is not a quieter
// yes: the seams are still answered from the settings that are there, so a
// development boot on an incomplete aws profile says local-disk, in-process and
// mailbox out loud rather than letting the profile name a cloud it was never given.
//
// An unknown profile name is one refusal and nothing else. Every other answer would
// be a guess about which table the deployment meant.
func resolveCloud(d Deployment) (cloudPlan, []issue) {
	var issues []issue
	p := cloudPlan{}
	if err := config.ValidateProfile(d.Config.Cloud.Profile); err != nil {
		return p, []issue{{method: "Deployment", err: err}}
	}
	// The configuration arrives as the profile resolved it: resolve applies the
	// profile once, before any module reads a setting, so the seams answered here are
	// the settings the composition builds itself from rather than a second reading of
	// them. See resolve.
	warn := d.Environment == Development
	profile := d.Config.Cloud.Profile
	if profile == "" {
		profile = config.ProfileLocal
	}
	p.profile = profile

	// Presence, one sentence per missing input. Collected rather than first-won:
	// the fix is one file with several lines in it, and a boot that names one per
	// run is a boot nobody finishes.
	unmet := map[string]bool{}
	for _, key := range d.Config.CloudUnmet(d.Config.Cloud.Profile) {
		unmet[key] = true
		sentence := seamNeeds(seamOfKey[key], key, d.Environment)
		if warn {
			p.warnings = append(p.warnings, sentence)
			continue
		}
		issues = append(issues, issue{method: "Deployment", err: fmt.Errorf("%s", sentence)})
	}

	// Storage is the one seam a profile can decline to name at all, and the one
	// mistake that has to be impossible: a deployment that chose a cloud and got
	// the disk. Both sentences are collected, because under azure they have one
	// fix and the deployment should hear both.
	if d.Config.Files.Store != config.StoreS3 {
		// The azure sentence is about a store nobody chose: an empty files.store is the
		// disk the profile was never allowed to name, and a disk the deployment named
		// itself is the deployment's own answer, which no profile contradicts.
		if profile == config.ProfileAzure && d.Config.Files.Store == "" {
			if warn {
				p.warnings = append(p.warnings, azureStorageSentence)
			} else {
				issues = append(issues, issue{method: "Deployment", err: errors.New(azureStorageSentence)})
			}
		}
		// An unread key says what it says whoever wrote it: the deployment meant to
		// use a store the composition was never given. Under a profile that had not
		// yet been given the address this is a person mid-setup in development, which
		// is why it warns there and refuses anywhere else, in the same shape as the
		// missing inputs above.
		if keys := d.Config.Files.S3Keys(); len(keys) > 0 {
			sentence := s3NotWired(keys)
			if warn {
				p.warnings = append(p.warnings, sentence)
			} else {
				issues = append(issues, issue{method: "Deployment", err: errors.New(sentence)})
			}
		}
	}

	for _, def := range seams {
		st := seamState{
			seam:     def.seam,
			provider: def.provider(d.Config),
			served:   def.note == "",
			note:     def.note,
		}
		st.keys = profileWrites(profile, def.seam, st.provider)
		switch {
		case st.note == notACloudSeam:
			st.source = sourceKernel
		case len(st.keys) > 0:
			st.source = sourceProfile
		case !def.decided(d.Config):
			st.source = sourceDefault
		default:
			st.source = sourceDeployment
		}
		if def.seam == Storage && profile == config.ProfileAzure && d.Config.Files.Store != config.StoreS3 {
			st.served, st.note = false, azureNamesNothing
		}
		p.seams = append(p.seams, st)
	}

	// The profile lost, once per key it would have written. This is the only place
	// the override is said, and it is said at boot rather than in Explain because
	// the deployment chose the value and the composition should not read as
	// disagreeing with the file it was started from.
	for _, cell := range config.CloudProfile(profile) {
		if cellUnmet(cell, unmet) {
			continue
		}
		for _, w := range cell.Sets {
			got, ok := resolvedKey(w.Key, d.Config)
			if !ok || got == w.Value {
				continue
			}
			p.warnings = append(p.warnings, fmt.Sprintf(
				"cloud profile %s sets %s to %s; this deployment names %s and keeps what it wrote",
				profile, w.Key, w.Value, w.Key))
		}
	}
	return p, issues
}

// cellUnmet reports whether a cell's seam is still missing something the profile
// asked for. A cell whose seam is under-supplied wrote nothing, so it overrides
// nothing either, and saying so twice would be one mistake with two sentences.
func cellUnmet(cell config.CloudCell, unmet map[string]bool) bool {
	for _, key := range cell.Required {
		if unmet[key] {
			return true
		}
	}
	return false
}

// profileWrites answers which keys of one seam the profile put there: the keys it
// declares for a seam whose resolved answer is the answer the profile writes. This
// is read from the resolved configuration rather than remembered through Load so
// that a Config built in Go — a test's, or a composition's own — resolves to the
// same seams as the same bytes in a file, which is what lets a deployment move its
// configuration into a secret store without the explanation surface changing text.
func profileWrites(profile string, s Seam, provider string) []string {
	var keys []string
	for _, cell := range config.CloudProfile(profile) {
		for _, w := range cell.Sets {
			if seamOfKey[w.Key] == s && w.Value == provider {
				keys = append(keys, w.Key)
			}
		}
	}
	return keys
}

// saySeams renders the seams as sentences: one for the profile, one per seam.
func saySeams(b *strings.Builder, cloud *DescribedCloud) {
	if cloud == nil {
		return
	}
	fmt.Fprintf(b, "pkit: cloud profile %s in %s.\n", cloud.Profile, cloud.Environment)
	for _, s := range cloud.Seams {
		paren := s.Source
		if s.Note != "" {
			paren += "; " + s.Note
		}
		fmt.Fprintf(b, "pkit: seam %s runs %s (%s).\n", s.Seam, s.Provider, paren)
	}
}
