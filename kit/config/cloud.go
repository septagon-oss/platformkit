package config

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// The cloud profile: one setting that answers the infrastructure questions a
// deployment answers the same way every time.
//
// A profile is data and one step inside Load: it writes the adapter a seam runs
// on and names what the deployment still owes. It never dials, never infers a
// vendor from a hostname and never writes a credential; enforcement of what it
// demands lives in pkit, which knows the environment and prints the answer.
//
//	file → composition Set → environment → profile fill → required keys → validation
//
// The fill writes only keys still empty, so it is the lowest layer, and it writes
// a seam's keys all-or-nothing: a deployment that named half of a seam is still
// deciding, and finishing the decision for it would hide the half-empty seam from
// the surface's own guard. No vendor endpoint appears below (decision 0038).

// validate refuses a profile name the table does not hold.
func (c Cloud) validate() error { return ValidateProfile(c.Profile) }

// The five profiles cloud.profile may name. They are infrastructure vocabulary
// and not environments: the same profile runs under development, staging or
// production, and the environment changes only what is refused for it.
const (
	ProfileLocal      = "local"
	ProfileAWS        = "aws"
	ProfileGCP        = "gcp"
	ProfileAzure      = "azure"
	ProfileSelfHosted = "self-hosted"
)

// The two object stores files.store may name. An empty setting is local-disk:
// the disk adapter is what this kernel has always wired, and a deployment that
// says nothing keeps it. The empty spelling is deliberately not rewritten to
// "local-disk" the way files.dir's default is — an operator who chose the disk
// and an operator who chose nothing are two different answers, and a profile
// that points a deployment at the disk has to be able to say it did not.
const (
	StoreLocalDisk = "local-disk"
	StoreS3        = "s3"
)

// The two shape defaults Load applies to an object store's settings. They are
// defaults and not profile cells: a cell that repeated a default would produce an
// override warning that means nothing.
const (
	S3SchemeHTTP        = "http"
	S3SchemeHTTPS       = "https"
	BucketLookupAuto    = "auto"
	BucketLookupDNS     = "dns"
	BucketLookupPath    = "path"
	defaultS3Scheme     = S3SchemeHTTPS
	defaultBucketLookup = BucketLookupAuto
)

// Cloud is the deployment's one infrastructure choice, written once.
//
// The empty profile is local: every seam runs on the adapter this kernel has
// always used and nothing is required of the deployment. That is an assertion
// that the configuration file is the whole truth, not a guess at where the
// process is running — the kernel reads no metadata address and no hostname to
// decide it.
type Cloud struct {
	Profile string `yaml:"profile"`
}

// CloudCell is one seam's worth of one cloud profile: the settings the profile
// writes once the deployment has named every setting the seam `Required`, and
// nothing at all until it has. A cell with no Required writes unconditionally,
// and a cell with no Sets demands without writing — which is what a seam whose
// adapter is already the only one there is needs.
//
// Keys are named here by the name the file, `Set` and the environment use, and
// are addressed through the same table an override goes through: a key the
// surface drops stops being writable here in the same commit.
type CloudCell struct {
	Sets     []CloudWrite
	Required []string
}

// CloudWrite is one key a profile fills and the value it fills it with.
type CloudWrite struct {
	Key   string
	Value string
}

// profiles lists every name cloud.profile may answer, in the order a refusal
// reads them out.
var profiles = []string{ProfileLocal, ProfileAWS, ProfileGCP, ProfileAzure, ProfileSelfHosted}

// cloudProfiles is the table: per profile, per seam, what it writes and what the
// deployment must still name.
//
// `local` holds no cells at all — it is the profile that requires nothing
// anywhere, in every environment. `azure` holds no storage cell: the object
// store this build links speaks S3 and Azure Blob does not, so the profile
// refuses to name a provider there rather than quietly pointing the deployment at
// the disk. pkit says that in one sentence and gates it by environment; this
// table only declines to write a key it has no business writing.
//
// The gcp storage cell is served through the S3-compatible endpoint and is
// unproven from this repository: no test here reaches that endpoint, so nothing
// here may claim it works.
var cloudProfiles = map[string][]CloudCell{
	ProfileLocal: nil,
	ProfileAWS: {
		{Sets: []CloudWrite{{StoreS3Key, StoreS3}}, Required: []string{S3EndpointKey, S3BucketKey, S3RegionKey, S3AccessKeyKey, S3SecretKeyKey}},
		valkeyCell, jetstreamCell, mailCell, telemetryCell,
	},
	ProfileGCP: {
		{Sets: []CloudWrite{{StoreS3Key, StoreS3}}, Required: []string{S3EndpointKey, S3BucketKey, S3AccessKeyKey, S3SecretKeyKey}},
		valkeyCell, jetstreamCell, mailCell, telemetryCell,
	},
	ProfileAzure: {valkeyCell, jetstreamCell, mailCell, telemetryCell},
	ProfileSelfHosted: {
		{Sets: []CloudWrite{{StoreS3Key, StoreS3}}, Required: []string{S3EndpointKey, S3BucketKey}},
		valkeyCell, jetstreamCell, mailCell, telemetryCell,
	},
}

// The four cells every profile but local carries. Each names the adapter and the
// two settings the deployment has to add for that adapter to run at all.
var (
	valkeyCell = CloudCell{
		Sets:     []CloudWrite{{CacheAdapterKey, "valkey"}},
		Required: []string{CacheURLKey, CacheAppKey},
	}
	jetstreamCell = CloudCell{Sets: []CloudWrite{{NATSTransportKey, "jetstream"}}}
	mailCell      = CloudCell{Required: []string{"mail.host", "mail.from"}}
	telemetryCell = CloudCell{Required: []string{"telemetry.otlp_endpoint"}}
)

// The key names the table addresses. They are named once, here, because pkit's
// seam table and the tests both read them and a spelling that drifts between the
// two is a seam that silently demands nothing.
const (
	StoreS3Key       = "files.store"
	S3EndpointKey    = "files.s3.endpoint"
	S3BucketKey      = "files.s3.bucket"
	S3RegionKey      = "files.s3.region"
	S3SchemeKey      = "files.s3.scheme"
	S3BucketLookup   = "files.s3.bucket_lookup"
	S3AccessKeyKey   = "files.s3.access_key"
	S3SecretKeyKey   = "files.s3.secret_key"
	CacheAdapterKey  = "cache.adapter"
	CacheURLKey      = "cache.url"
	CacheAppKey      = "cache.app"
	NATSTransportKey = "nats.transport"
)

// Profiles names every profile cloud.profile may hold, in the order the refusal
// reads them.
func Profiles() []string { return profiles }

// CloudProfile answers one profile's cells, in table order, and nil for a name
// the table does not hold. The slice is the kernel's and is never written
// through: a caller reads what a profile asks, it does not negotiate it.
func CloudProfile(name string) []CloudCell { return cloudProfiles[name] }

// KnownProfile reports whether name is one of the five. The empty name is not a
// profile a deployment named — it is the absence of one, which resolves to local
// and is refused nowhere.
func KnownProfile(name string) bool {
	return name == "" || slices.Contains(profiles, name)
}

// ValidateProfile refuses a profile name the table does not hold, in the one
// sentence both doors print.
//
// Both doors need it because a Config reaches a deployment two ways — through
// Load, and built in Go by a composition or a test — and `Explain` never calls
// Load. A typo would otherwise answer for the local profile in the one reading
// where no profile was resolved at all.
func ValidateProfile(name string) error {
	if KnownProfile(name) {
		return nil
	}
	return fmt.Errorf("cloud.profile is %q; name %s", name, orList(profiles))
}

// ApplyCloudProfile answers the configuration the named profile resolves to: every
// seam cell the profile declares, written into the keys that are still empty, and
// nothing else. It is the same step Load runs, on a copy, and it is idempotent, so
// running it again changes nothing — which is why it can be run both when a file is
// read and when a configuration is read back.
//
// It is exported for that second reading. A Config also arrives built in Go, by a
// composition or a test, and pkit answers Explain, Describe and Validate about
// those without a Load in the path: a profile that resolved only on the way in from
// a file would leave the same deployment described two ways depending on how it
// reached the process.
func ApplyCloudProfile(c Config) Config {
	c.applyCloud(c.Cloud.Profile)
	return c
}

// applyCloud fills the seams the named profile declares, under the rule the
// package comment gives: a cell writes only when the deployment already named
// every key that cell requires, and only into keys that are still empty.
func (c *Config) applyCloud(profile string) {
	for _, cell := range cloudProfiles[profile] {
		if !cellNamed(c, cell) {
			continue
		}
		for _, w := range cell.Sets {
			if f := stringKey(c, w.Key); f != nil && *f == "" {
				*f = w.Value
			}
		}
	}
}

// cellNamed reports whether the deployment gave every input that seam needs
// before the profile touches it. A key the surface no longer has counts as not
// given: the profile writes nothing it cannot address.
func cellNamed(c *Config, cell CloudCell) bool {
	for _, name := range cell.Required {
		f := stringKey(c, name)
		if f == nil || *f == "" {
			return false
		}
	}
	return true
}

// CloudUnmet answers, in table order, the keys the named profile demands of this
// deployment that are still empty. It is the question pkit asks to refuse a
// deployment that picked a profile and did not finish it, and it answers all of
// them at once rather than the first: the fix is one file with several lines in
// it, and a boot that names one per run is a boot nobody finishes.
//
// A seam the deployment answered with another adapter demands nothing. The
// deployment overrode the profile there and runs something else, so the settings
// that adapter would have needed are not needs of this deployment — a profile may
// replace a deployment's answer only where the deployment left it open.
func (c Config) CloudUnmet(profile string) []string {
	var unmet []string
	for _, cell := range cloudProfiles[profile] {
		if cellOverridden(&c, cell) {
			continue
		}
		for _, name := range cell.Required {
			if f := stringKey(&c, name); f == nil || *f == "" {
				unmet = append(unmet, name)
			}
		}
	}
	return unmet
}

// cellOverridden reports whether this configuration names an adapter for the
// seam the cell writes that is not the adapter the cell writes. The check is on
// what the deployment wrote, not on what the profile filled: a cell that wrote
// its own answer leaves that answer in the key, and a cell whose seam nobody
// answered leaves the key empty — neither is an override.
//
// A cell that writes no adapter demands what every deployment needs (a mail host
// is a mail host whichever cache runs) and can never be overridden here.
func cellOverridden(c *Config, cell CloudCell) bool {
	for _, w := range cell.Sets {
		f := stringKey(c, w.Key)
		if f != nil && *f != "" && *f != w.Value {
			return true
		}
	}
	return false
}

// stringKey is where a key name reaches a Config: the same table an override and
// the environment go through, so a name all three share is one name.
func stringKey(c *Config, name string) *string {
	i := slices.IndexFunc(keys, func(k key) bool { return k.name == name })
	if i < 0 {
		return nil
	}
	return keys[i].field(c)
}

// Keys lists every key a deployment, a composition or the environment may set by
// name — the list the refusal prints when a name is not one of them, and the list
// a caller walks to be sure a table of keys names keys the surface still has.
func Keys() []string {
	names := make([]string, len(keys))
	for j, k := range keys {
		names[j] = k.name
	}
	return names
}

// orList joins names with commas and an "or" before the last.
func orList(ss []string) string {
	switch len(ss) {
	case 0:
		return ""
	case 1:
		return ss[0]
	case 2:
		return ss[0] + " or " + ss[1]
	default:
		return strings.Join(ss[:len(ss)-1], ", ") + " or " + ss[len(ss)-1]
	}
}

// validateObjects refuses the two object-store settings that are malformed on
// their own, and applies the two shape defaults. Presence is not refused here:
// whether a seam is under-supplied is the profile's question, and the profile is
// environment-dependent, which this package is not.
func (f *Files) validateObjects() error {
	if f.Store != "" && f.Store != StoreLocalDisk && f.Store != StoreS3 {
		return fmt.Errorf("files.store is %q; it is %s, %s or empty for %s",
			f.Store, StoreLocalDisk, StoreS3, StoreLocalDisk)
	}
	if s := f.S3.Scheme; s != "" && s != S3SchemeHTTP && s != S3SchemeHTTPS {
		return fmt.Errorf("files.s3.scheme is %q; an object store is reached over %s or %s", s, S3SchemeHTTP, S3SchemeHTTPS)
	}
	if l := f.S3.BucketLookup; l != "" && l != BucketLookupAuto && l != BucketLookupDNS && l != BucketLookupPath {
		return fmt.Errorf("files.s3.bucket_lookup is %q; it is %s, %s or %s", l, BucketLookupAuto, BucketLookupDNS, BucketLookupPath)
	}
	// The endpoint is a host and nothing else, because the client this seam builds
	// takes it as the argument it dials and a scheme pasted onto it becomes a
	// request to a host named "". The grammar is nats.url's and cache.url's: one
	// sentence naming the key, because the fix is always the same edit.
	if f.S3.Endpoint != "" && !validHost(f.S3.Endpoint) {
		return errors.New("files.s3.endpoint must be host:port without a scheme, path, query or credentials; use files.s3.scheme for the scheme and the two key names for credentials")
	}
	// minio-go answers an empty pair as *anonymous*: NewStaticV4 with no values
	// returns credentials that sign nothing, so half a pair is not a partially
	// authenticated store, it is an unauthenticated one that reads like a
	// configured one. That is why the pair is refused apart from any profile.
	if (f.S3.AccessKey == "") != (f.S3.SecretKey == "") {
		return errors.New("files.s3.access_key and files.s3.secret_key must both be set or both be empty")
	}
	if f.S3.Scheme == "" {
		f.S3.Scheme = defaultS3Scheme
	}
	if f.S3.BucketLookup == "" {
		f.S3.BucketLookup = defaultBucketLookup
	}
	return nil
}

// S3Keys answers which of the object-store keys the deployment itself wrote. The
// two shape keys are not asked: Load gives both a value whatever the deployment
// wrote, so neither says anything about intent, and what an unread key proves is
// that somebody meant to use a store.
func (f Files) S3Keys() []string {
	var set []string
	for _, k := range []struct {
		name, value string
	}{
		{S3EndpointKey, f.S3.Endpoint},
		{S3BucketKey, f.S3.Bucket},
		{S3RegionKey, f.S3.Region},
		{S3AccessKeyKey, f.S3.AccessKey},
		{S3SecretKeyKey, f.S3.SecretKey},
	} {
		if k.value != "" {
			set = append(set, k.name)
		}
	}
	return set
}
