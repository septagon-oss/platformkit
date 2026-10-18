package pkit_test

// The seams, answered through the same resolution Validate answers, Explain
// prints and Build runs: no fake sits between the assertion and the thing under
// test, and nothing here opens anything — Describe reads a Deployment's
// configuration and no connection.

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/cart"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/user"
	"github.com/septagon-oss/platformkit/pkit/internal/fixture/wishlist"
)

// cloudApp is the smallest composition this package's tests resolve: three
// fixture modules and no process.
func cloudApp() *pkit.App {
	return pkit.NewApp("collect").Use(user.Module, wishlist.Module, cart.Module)
}

// complete names every setting any profile demands, so a case about one seam is
// not answered by another seam's missing input.
func complete(profile, store string) config.Config {
	c := config.Config{}
	c.Cloud.Profile = profile
	c.Files.Store = store
	c.Files.S3.Endpoint = "s3.example.com:443"
	c.Files.S3.Bucket = "uploads"
	c.Files.S3.Region = "eu-west-1"
	c.Files.S3.AccessKey = "AKIA"
	c.Files.S3.SecretKey = "hunter2"
	c.Cache.URL = "valkey://cache.example.com:6379"
	c.Cache.App = "collect"
	c.Mail.Host = "smtp.example.com"
	c.Mail.From = "noreply@example.com"
	c.Telemetry.OTLPEndpoint = "https://collector.example.com:4318"
	return c
}

// sentences is Describe's machine view of a refusal: the problem sentences, sorted,
// so an extra one fails the case and a reworded one fails it too.
func sentences(t *testing.T, d pkit.Deployment) []string {
	t.Helper()
	doc, _ := cloudApp().Describe(d)
	var out []string
	for _, p := range doc.Problems {
		out = append(out, p.Sentence)
	}
	slices.Sort(out)
	return out
}

// seam answers one seam of the resolved document; it fails the case when the
// document carries no seams, which is what a refusal means.
func seam(t *testing.T, d pkit.Deployment, name pkit.Seam) pkit.DescribedSeam {
	t.Helper()
	doc, err := cloudApp().Describe(d)
	if err != nil {
		t.Fatalf("the deployment was refused: %v", err)
	}
	if doc.Cloud == nil {
		t.Fatal("the document carries no seams")
	}
	for _, s := range doc.Cloud.Seams {
		if s.Seam == name {
			return s
		}
	}
	t.Fatalf("no seam %s among %d", name, len(doc.Cloud.Seams))
	return pkit.DescribedSeam{}
}

// TestEveryProfileNamesAProviderForEverySeam is the matrix: five profiles, three
// environments, every seam answered in every cell. Each configuration satisfies
// every input the profile demands, so a cell that answers the adapter's default
// rather than the profile's is a wrong answer and not a missing input.
func TestEveryProfileNamesAProviderForEverySeam(t *testing.T) {
	for _, profile := range config.Profiles() {
		// Each cell's configuration satisfies every input the profile demands, so a
		// seam that answered its adapter's default rather than the profile's is a
		// wrong answer and not a missing input. local is the profile that writes
		// nothing, so its configuration names nothing either.
		cfg := complete(profile, config.StoreS3)
		want := map[pkit.Seam]string{
			pkit.Storage: "s3", pkit.Cache: "valkey", pkit.Events: "jetstream",
			pkit.Mail: "smtp", pkit.Policy: "rego-in-process", pkit.Telemetry: "otlp",
			pkit.Identity: "password-only", pkit.Flags: "configuration",
		}
		if profile == config.ProfileLocal {
			cfg = config.Config{}
			want[pkit.Storage], want[pkit.Cache], want[pkit.Events] = "local-disk", "in-process", "by-role"
			want[pkit.Mail], want[pkit.Telemetry] = "mailbox", "none"
		}
		for _, env := range []pkit.Environment{pkit.Development, pkit.Staging, pkit.Production} {
			d := pkit.Deployment{Environment: env, Config: cfg}
			doc, err := cloudApp().Describe(d)
			if err != nil {
				t.Fatalf("%s/%s was refused: %v", profile, env, err)
			}
			if doc.Cloud.Profile != profileOf(profile) {
				t.Errorf("%s/%s resolves profile %q", profile, env, doc.Cloud.Profile)
			}
			if len(doc.Cloud.Seams) != 8 {
				t.Fatalf("%s/%s answers %d seams", profile, env, len(doc.Cloud.Seams))
			}
			for _, s := range doc.Cloud.Seams {
				if s.Provider == "" || s.Provider != want[s.Seam] {
					t.Errorf("%s/%s: seam %s runs %q, not %q", profile, env, s.Seam, s.Provider, want[s.Seam])
				}
				if !s.Served && s.Note == "" {
					t.Errorf("%s/%s: seam %s is unserved and says nothing about it", profile, env, s.Seam)
				}
			}
		}
	}
}

// profileOf is the resolved name: an empty setting is local, and this is where the
// test says so rather than assuming it.
func profileOf(profile string) string {
	if profile == "" {
		return config.ProfileLocal
	}
	return profile
}

// TestAProfileThatLacksItsInputsRefusesOncePerInput is the acceptance line: a
// production deployment that chose aws and named nothing about storage, cache,
// mail or telemetry is told about all ten settings in one answer.
func TestAProfileThatLacksItsInputsRefusesOncePerInput(t *testing.T) {
	got := sentences(t, pkit.Deployment{Environment: pkit.Production,
		Config: config.Config{Cloud: config.Cloud{Profile: config.ProfileAWS}}})
	want := []string{
		"cache needs cache.app in production: set it, or the deployment cannot serve cache there",
		"cache needs cache.url in production: set it, or the deployment cannot serve cache there",
		"mail needs mail.from in production: set it, or the deployment cannot serve mail there",
		"mail needs mail.host in production: set it, or the deployment cannot serve mail there",
		"storage needs files.s3.access_key in production: set it, or the deployment cannot serve storage there",
		"storage needs files.s3.bucket in production: set it, or the deployment cannot serve storage there",
		"storage needs files.s3.endpoint in production: set it, or the deployment cannot serve storage there",
		"storage needs files.s3.region in production: set it, or the deployment cannot serve storage there",
		"storage needs files.s3.secret_key in production: set it, or the deployment cannot serve storage there",
		"telemetry needs telemetry.otlp_endpoint in production: set it, or the deployment cannot serve telemetry there",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("aws in production did not say all ten:\n%s", strings.Join(got, "\n"))
	}
}

// TestDevelopmentWarnsWhereStagingRefuses is the gate by environment. Warning is
// not a quieter yes: the seams answer the adapters that are actually wired, so a
// half-set-up laptop reads local-disk and not the cloud it asked for.
func TestDevelopmentWarnsWhereStagingRefuses(t *testing.T) {
	bare := config.Config{Cloud: config.Cloud{Profile: config.ProfileAWS}}
	if got := sentences(t, pkit.Deployment{Environment: pkit.Development, Config: bare}); len(got) != 0 {
		t.Errorf("development refused instead of warning:\n%s", strings.Join(got, "\n"))
	}
	for _, env := range []pkit.Environment{pkit.Staging, pkit.Production} {
		if got := sentences(t, pkit.Deployment{Environment: env, Config: bare}); len(got) != 10 {
			t.Errorf("%s refused %d settings, not 10", env, len(got))
		}
	}
	d := pkit.Deployment{Environment: pkit.Development, Config: bare}
	for name, want := range map[pkit.Seam]string{
		pkit.Storage: "local-disk", pkit.Cache: "in-process", pkit.Mail: "mailbox", pkit.Telemetry: "none",
	} {
		if got := seam(t, d, name); got.Provider != want {
			t.Errorf("development seam %s runs %q, not %q: warned is not silently cloud", name, got.Provider, want)
		}
	}
}

// TestAzureRefusesStorageByNameAndNeverPointsItAtDisk: the profile that cannot name
// an object store says so rather than quietly resolving to the directory.
func TestAzureRefusesStorageByNameAndNeverPointsItAtDisk(t *testing.T) {
	cfg := complete(config.ProfileAzure, "")
	cfg.Files.S3 = config.S3{}
	d := pkit.Deployment{Environment: pkit.Production, Config: cfg}
	got := sentences(t, d)
	if len(got) != 1 || !strings.HasPrefix(got[0], "cloud profile azure names no storage provider") {
		t.Fatalf("azure did not refuse storage by name:\n%s", strings.Join(got, "\n"))
	}
	if cfg.Files.Store != "" {
		t.Errorf("files.store resolved to %q: the refusal may not point the deployment at the disk", cfg.Files.Store)
	}
	doc, _ := cloudApp().Describe(d)
	if doc.Resolved || doc.Cloud != nil {
		t.Errorf("a refused deployment described a composition anyway: %+v", doc.Cloud)
	}
}

// TestAnAzureDeploymentThatNamesItsOwnStoreIsServed: the refusal is about a profile
// that names nothing, and an operator who named the store themselves is answered by
// the store they named, with no warning beside it.
func TestAnAzureDeploymentThatNamesItsOwnStoreIsServed(t *testing.T) {
	d := pkit.Deployment{Environment: pkit.Production, Config: complete(config.ProfileAzure, config.StoreS3)}
	if got := sentences(t, d); len(got) != 0 {
		t.Fatalf("an azure deployment that named its own store was refused:\n%s", strings.Join(got, "\n"))
	}
	s := seam(t, d, pkit.Storage)
	if s.Provider != "s3" || s.Source != "deployment" || !s.Served {
		t.Errorf("azure storage resolved to %+v; the deployment wrote it, so the source is the deployment", s)
	}
}

// TestSelfHostedRequiresTheEndpointsItRunsAgainst: a store and a broker on a network
// of the deployment's own are still addresses somebody has to write.
func TestSelfHostedRequiresTheEndpointsItRunsAgainst(t *testing.T) {
	got := sentences(t, pkit.Deployment{Environment: pkit.Production,
		Config: config.Config{Cloud: config.Cloud{Profile: config.ProfileSelfHosted}}})
	if len(got) != 7 {
		t.Fatalf("self-hosted in production refused %d settings, not 7:\n%s", len(got), strings.Join(got, "\n"))
	}
	for _, sentence := range got {
		if !strings.Contains(sentence, " in production: set it, or the deployment cannot serve ") {
			t.Errorf("the sentence is not the profile's own grammar: %s", sentence)
		}
	}
}

// TestLocalRefusesNothingAnywhere is the profile that asserts the file is the whole
// truth: it demands nothing, in production included.
func TestLocalRefusesNothingAnywhere(t *testing.T) {
	for _, env := range []pkit.Environment{pkit.Development, pkit.Staging, pkit.Production} {
		d := pkit.Deployment{Environment: env, Config: config.Config{}}
		if got := sentences(t, d); len(got) != 0 {
			t.Errorf("local in %s refused: %s", env, strings.Join(got, "\n"))
		}
		doc, _ := cloudApp().Describe(d)
		for _, s := range doc.Cloud.Seams {
			if len(s.Keys) != 0 {
				t.Errorf("local in %s claims it wrote %v on seam %s", env, s.Keys, s.Seam)
			}
		}
	}
}

// TestASeamTheProfileDoesNotServeRefusesKeysThatNameOne is the unread-key refusal:
// one sentence naming every key, in the environments where a deployment is expected
// to be finished, and no sentence at all once the store is wired or never written.
func TestASeamTheProfileDoesNotServeRefusesKeysThatNameOne(t *testing.T) {
	names := "files.s3.endpoint, files.s3.bucket, files.s3.region names an S3 store the composition does not wire; set files.store to s3"
	cfg := config.Config{}
	cfg.Files.S3.Endpoint, cfg.Files.S3.Bucket, cfg.Files.S3.Region = "s3.example.com:443", "uploads", "eu-west-1"
	for _, env := range []pkit.Environment{pkit.Staging, pkit.Production} {
		got := sentences(t, pkit.Deployment{Environment: env, Config: cfg})
		if len(got) != 1 || got[0] != names {
			t.Errorf("%s said %q, not the one sentence about the three keys", env, strings.Join(got, " | "))
		}
	}
	if got := sentences(t, pkit.Deployment{Environment: pkit.Development, Config: cfg}); len(got) != 0 {
		t.Errorf("development refused an unread key instead of warning about it: %s", strings.Join(got, "\n"))
	}
	// Wired, the keys are read and nothing is left over.
	wired := cfg
	wired.Files.Store = config.StoreS3
	if got := sentences(t, pkit.Deployment{Environment: pkit.Production, Config: wired}); len(got) != 0 {
		t.Errorf("a store the composition wires is still refused: %s", strings.Join(got, "\n"))
	}
	// And the refusal is about the keys, not about the profile that served no store.
	if got := sentences(t, pkit.Deployment{Environment: pkit.Production, Config: config.Config{}}); len(got) != 0 {
		t.Errorf("a deployment that wrote nothing was refused for it: %s", strings.Join(got, "\n"))
	}
}

// TestAProfileOutsideTheFiveIsRefusedByTheResolver is the door Load cannot stand in
// for: Explain, Validate and Describe never call it, so a Config built in Go is
// refused by the same sentence.
func TestAProfileOutsideTheFiveIsRefusedByTheResolver(t *testing.T) {
	got := sentences(t, pkit.Deployment{Environment: pkit.Production,
		Config: config.Config{Cloud: config.Cloud{Profile: "digitalocean"}}})
	want := `cloud.profile is "digitalocean"; name local, aws, gcp, azure or self-hosted`
	if len(got) != 1 || got[0] != want {
		t.Errorf("a profile outside the five answered %q, not %q", strings.Join(got, "\n"), want)
	}
	if err := cloudApp().Validate(pkit.Deployment{Environment: pkit.Production,
		Config: config.Config{Cloud: config.Cloud{Profile: "digitalocean"}}}); err == nil {
		t.Error("Validate accepted a profile outside the five")
	}
}

// TestTheProfileTableDemandsOnlyKeysASeamAnswers is the seam between two tables that
// have to agree: kit/config names settings, pkit names the seam that owes each one,
// and a demand nobody maps to a seam would be a sentence about an empty seam.
func TestTheProfileTableDemandsOnlyKeysASeamAnswers(t *testing.T) {
	for _, profile := range config.Profiles() {
		for _, cell := range config.CloudProfile(profile) {
			for _, name := range append(append([]string{}, cell.Required...), setKeys(cell)...) {
				d := pkit.Deployment{Environment: pkit.Production,
					Config: config.Config{Cloud: config.Cloud{Profile: profile}}}
				for _, s := range sentences(t, d) {
					if strings.Contains(s, " needs "+name+" ") && strings.HasPrefix(s, " ") {
						t.Errorf("%s: the sentence about %q names no seam: %q", profile, name, s)
					}
				}
			}
		}
	}
}

func setKeys(cell config.CloudCell) []string {
	var out []string
	for _, w := range cell.Sets {
		out = append(out, w.Key)
	}
	return out
}

// TestExplainNamesTheProviderOfEverySeam pins the prose byte for byte, because the
// same text is what a client commits as its composition file.
func TestExplainNamesTheProviderOfEverySeam(t *testing.T) {
	want := "pkit: cloud profile aws in production.\n" +
		"pkit: seam storage runs s3 (cloud profile).\n" +
		"pkit: seam cache runs valkey (cloud profile).\n" +
		"pkit: seam events runs jetstream (cloud profile).\n" +
		"pkit: seam mail runs smtp (deployment).\n" +
		"pkit: seam policy runs rego-in-process (kernel; not a cloud seam).\n" +
		"pkit: seam telemetry runs otlp (deployment).\n" +
		"pkit: seam identity runs password-only (default).\n" +
		"pkit: seam flags runs configuration (kernel; not a cloud seam).\n"
	text, err := cloudApp().Explain(pkit.Deployment{Environment: pkit.Production,
		Config: complete(config.ProfileAWS, config.StoreS3)})
	if err != nil {
		t.Fatalf("the complete deployment was refused: %v", err)
	}
	if !strings.Contains(text, want) {
		t.Errorf("the seam section is not these nine lines:\n%s", text)
	}
}

// TestTheSeamSectionIsTheSameFactsTheDocumentCarries is one projection, two
// encodings: the document the JSON reads and the prose a person reads answer the
// same nine facts.
func TestTheSeamSectionIsTheSameFactsTheDocumentCarries(t *testing.T) {
	d := pkit.Deployment{Environment: pkit.Production, Config: complete(config.ProfileAWS, config.StoreS3)}
	doc, err := cloudApp().Describe(d)
	if err != nil {
		t.Fatalf("the complete deployment was refused: %v", err)
	}
	text, err := cloudApp().Explain(d)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	for _, s := range doc.Cloud.Seams {
		line := "pkit: seam " + string(s.Seam) + " runs " + s.Provider + " (" + s.Source
		if s.Note != "" {
			line += "; " + s.Note
		}
		if !strings.Contains(text, line+").\n") {
			t.Errorf("the prose never says %q to a reader of %+v", line+".", s)
		}
	}
}

// TestTheSeamSectionCarriesKeyNamesAndNoValue: the document names the settings a
// profile wrote, and the values a deployment gave them appear nowhere in it.
func TestTheSeamSectionCarriesKeyNamesAndNoValue(t *testing.T) {
	cfg := complete(config.ProfileAWS, config.StoreS3)
	cfg.Files.S3.SecretKey = "a-secret-nobody-prints"
	cfg.Cache.Password = "another"
	doc, err := cloudApp().Describe(pkit.Deployment{Environment: pkit.Production, Config: cfg})
	if err != nil {
		t.Fatalf("the complete deployment was refused: %v", err)
	}
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, secret := range []string{"a-secret-nobody-prints", "another", "hunter2", "s3.example.com:443"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("the document carries the value %q", secret)
		}
	}
	if got := seam(t, pkit.Deployment{Environment: pkit.Production, Config: cfg}, pkit.Storage); !slices.Contains(got.Keys, "files.store") {
		t.Errorf("the storage seam does not name the key the profile wrote: %+v", got)
	}
}
