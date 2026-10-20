package config_test

// The cloud profile as the loader sees it: what it writes, what it refuses, and
// the two things it never does — write a credential, or outrank a value somebody
// already wrote.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
)

// writeConfig puts one YAML body in front of Load, which is the same door run,
// migrate, bootstrap, seed and start walk.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write the configuration: %v", err)
	}
	return path
}

// boots loads a deployment that names every seam's input, so a test that is about
// something else never trips over the inputs it did not bother to write.
func boots(t *testing.T, body string) config.Config {
	t.Helper()
	cfg, err := config.Load(writeConfig(t, body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

// full is the complete aws deployment: every key any profile demands, named.
const full = `server:
  addr: ":8080"
  public_host: "platformkit.localhost"
database:
  url: "postgres://platformkit_app:platformkit@localhost:5432/platformkit?sslmode=disable"
  migrate_url: "postgres://postgres:platformkit@localhost:5432/platformkit?sslmode=disable"
nats:
  url: "nats://localhost:4222"
log:
  level: "info"
webhook:
  secret_key: "0123456789abcdef0123456789abcdef"
`

// cloudOf is `full` with a profile and whatever else the case wants written.
func cloudOf(profile, extra string) string {
	return full + "cloud:\n  profile: \"" + profile + "\"\n" + extra
}

// awsInputs names every key the aws profile demands of a deployment.
const awsInputs = `files:
  s3:
    endpoint: "s3.example.com:443"
    bucket: "uploads"
    region: "eu-west-1"
    access_key: "AKIA"
    secret_key: "hunter2"
cache:
  url: "valkey://cache.example.com:6379"
  app: "platformkit"
mail:
  host: "smtp.example.com"
  from: "noreply@example.com"
telemetry:
  otlp_endpoint: "https://collector.example.com:4318"
`

// TestTheProfileWritesNoSecret walks the table itself rather than one deployment:
// a cell that named a credential would otherwise be discovered by the first
// installation whose secret landed in a committed file.
func TestTheProfileWritesNoSecret(t *testing.T) {
	// Read from the surface rather than copied in here, because a key the surface
	// dropped is a key a profile can no longer write, and a list pasted into a test
	// agrees with itself instead.
	surface := config.Keys()

	secrets := []string{
		"auth.oidc.client_secret", "auth.factor_key", "nats.username", "nats.password",
		"cache.password", "webhook.secret_key", "database.url", "database.migrate_url", "database.retain_url",
		"files.s3.secret_key", "files.s3.access_key", "bootstrap.password", "demo.password",
		"seed.operator_password", "secrets.token",
	}
	for _, profile := range config.Profiles() {
		for _, cell := range config.CloudProfile(profile) {
			for _, w := range cell.Sets {
				if !slices.Contains(surface, w.Key) {
					t.Errorf("profile %s writes %q, which the key surface does not have", profile, w.Key)
				}
				if slices.Contains(secrets, w.Key) {
					t.Errorf("profile %s writes the secret %s", profile, w.Key)
				}
			}
			for _, name := range cell.Required {
				if !slices.Contains(surface, name) {
					t.Errorf("profile %s requires %q, which the key surface does not have", profile, name)
				}
			}
		}
	}
}

// TestTheProfileSitsUnderTheFile is the layer below: a deployment that wrote an
// answer keeps it, whatever the profile would have written.
func TestTheProfileSitsUnderTheFile(t *testing.T) {
	cfg := boots(t, cloudOf(config.ProfileAWS, strings.Replace(awsInputs, "files:\n", "files:\n  store: \"local-disk\"\n", 1)))
	if cfg.Files.Store != config.StoreLocalDisk {
		t.Errorf("the file's own store is %q; the profile outranked it", cfg.Files.Store)
	}
	// The rest of the seams still fill: refusing to outrank one key is not the
	// same as refusing to write the others.
	if cfg.Cache.Adapter != "valkey" {
		t.Errorf("cache.adapter is %q; one override is not a withdrawal from the profile", cfg.Cache.Adapter)
	}
}

// TestTheEnvironmentOutranksTheProfile is the layer above. The word a deployment
// exported is the last word, which is what PLATFORMKIT_SERVER_PUBLIC_HOST has
// always been for a composition's own override.
func TestTheEnvironmentOutranksTheProfile(t *testing.T) {
	// The seam is named by the variable alone: a deployment that exported its own
	// adapter while leaving the shared store's address unwritten is a deployment
	// whose cache seam the surface has already refused, for the reason it states —
	// cache.url belongs to an adapter that is no longer valkey.
	t.Setenv("PLATFORMKIT_CACHE_ADAPTER", "memory")
	cfg := boots(t, cloudOf(config.ProfileAWS, "cache:\n  app: \"platformkit\"\nmail:\n  host: \"smtp.example.com\"\n  from: \"noreply@example.com\"\ntelemetry:\n  otlp_endpoint: \"https://collector.example.com:4318\"\nfiles:\n  s3:\n    endpoint: \"s3.example.com:443\"\n    bucket: \"uploads\"\n    region: \"eu-west-1\"\n    access_key: \"AKIA\"\n    secret_key: \"hunter2\"\n"))
	if cfg.Cache.Adapter != "memory" {
		t.Errorf("cache.adapter resolved to %q; the environment is not the last word", cfg.Cache.Adapter)
	}
	// One layer losing is not the profile withdrawing: the seams the variable said
	// nothing about still resolve to what the profile writes.
	if cfg.NATS.Transport != "jetstream" || cfg.Files.Store != "s3" {
		t.Errorf("the profile stopped at the first override: %q %q", cfg.NATS.Transport, cfg.Files.Store)
	}
}

// TestTheProfileIsAppliedOnceToItself is idempotence, said as the property it
// exists to keep: the fill writes only empty keys, so a configuration that has
// already been through it is unchanged by a second pass.
func TestTheProfileIsAppliedOnceToItself(t *testing.T) {
	once := boots(t, cloudOf(config.ProfileAWS, awsInputs))
	// The same deployment described the other way round, with the profile's own
	// three writes written out as the file's, in one block per seam rather than the
	// three blocks the filled deployment reads as.
	twice := boots(t, strings.Replace(cloudOf(config.ProfileAWS, "files:\n  store: \"s3\"\n  s3:\n    endpoint: \"s3.example.com:443\"\n    bucket: \"uploads\"\n    region: \"eu-west-1\"\n    access_key: \"AKIA\"\n    secret_key: \"hunter2\"\ncache:\n  adapter: \"valkey\"\n  url: \"valkey://cache.example.com:6379\"\n  app: \"platformkit\"\nmail:\n  host: \"smtp.example.com\"\n  from: \"noreply@example.com\"\ntelemetry:\n  otlp_endpoint: \"https://collector.example.com:4318\"\n"),
		`  url: "nats://localhost:4222"\n`, `  url: "nats://localhost:4222"\n  transport: "jetstream"\n`, 1))
	if once.Files.Store != twice.Files.Store || once.Cache.Adapter != twice.Cache.Adapter || once.NATS.Transport != twice.NATS.Transport {
		t.Errorf("the profile resolved twice into two deployments: %+v against %+v", once.Files, twice.Files)
	}
	if twice.NATS.Transport != "jetstream" || twice.Cache.Adapter != "valkey" || twice.Files.Store != "s3" {
		t.Errorf("the written-out deployment did not resolve to what the filled one says: %q %q %q",
			twice.NATS.Transport, twice.Cache.Adapter, twice.Files.Store)
	}
}

// TestTheS3ShapeIsCheckedAndNotItsPresence pins the boundary between the two
// voices: the surface refuses a value that is malformed on its own, whoever wrote
// it and whatever the profile is, and never a value that is merely absent.
func TestTheS3ShapeIsCheckedAndNotItsPresence(t *testing.T) {
	for _, tc := range []struct{ body, sentence string }{
		{"files:\n  store: \"ceph\"\n", `files.store is "ceph"; it is local-disk, s3 or empty for local-disk`},
		{"files:\n  s3:\n    scheme: \"ftp\"\n", `files.s3.scheme is "ftp"; an object store is reached over http or https`},
		{"files:\n  s3:\n    bucket_lookup: \"virtual\"\n", `files.s3.bucket_lookup is "virtual"; it is auto, dns or path`},
		{"files:\n  s3:\n    endpoint: \"https://s3.example.com/bucket\"\n", "files.s3.endpoint must be host:port without a scheme, path, query or credentials"},
		{"files:\n  s3:\n    access_key: \"AKIA\"\n", "files.s3.access_key and files.s3.secret_key must both be set or both be empty"},
		{"cloud:\n  profile: \"digitalocean\"\n", `cloud.profile is "digitalocean"; name local, aws, gcp, azure or self-hosted`},
	} {
		_, err := config.Load(writeConfig(t, full+tc.body))
		if err == nil || !strings.Contains(err.Error(), tc.sentence) {
			t.Errorf("%s does not refuse with %q:\n%v", tc.body, tc.sentence, err)
		}
	}
	// A store block that is present and empty is a deployment that wrote nothing,
	// and it answers the surface's own defaults rather than a refusal.
	cfg := boots(t, full+"files:\n  store: \"\"\n  s3:\n    endpoint: \"\"\n")
	if cfg.Files.S3.Scheme != "https" || cfg.Files.S3.BucketLookup != "auto" {
		t.Errorf("an empty s3 block resolved to %q/%q; the two shape defaults are not presence checks",
			cfg.Files.S3.Scheme, cfg.Files.S3.BucketLookup)
	}
	if cfg.Files.Store != "" {
		t.Errorf("files.store resolved to %q; the disk is the default, and writing the word would erase the difference between the deployment that chose it and the one that said nothing", cfg.Files.Store)
	}
}

// TestAHalfNamedSeamIsRefusedByTheSurfaceThatGuardsIt is the boundary case the
// fill rule buys: a seam the deployment named half of keeps the surface's own
// sentence about it, which is the sentence that names what to add.
func TestAHalfNamedSeamIsRefusedByTheSurfaceThatGuardsIt(t *testing.T) {
	_, err := config.Load(writeConfig(t, cloudOf(config.ProfileAWS, "cache:\n  url: \"valkey://cache.example.com:6379\"\n")))
	want := "cache.url and cache.password belong to cache.adapter valkey"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("a half-named cache seam did not reach the guard that owns the sentence %q:\n%v", want, err)
	}
}

// TestAProfileOutsideTheFiveIsRefusedByTheLoader is one of the two doors; pkit's
// cloud test is the other, for a Config nobody loaded.
func TestAProfileOutsideTheFiveIsRefusedByTheLoader(t *testing.T) {
	_, err := config.Load(writeConfig(t, cloudOf("oracle", awsInputs)))
	if err == nil || !strings.Contains(err.Error(), `cloud.profile is "oracle"`) {
		t.Fatalf("a profile outside the five loaded:\n%v", err)
	}
}

// TestTheExampleCarriesTheProfile keys and example file part company in the one
// file a person copies, so the example is loaded here as the file it is.
func TestTheExampleCarriesTheProfile(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "config.example.yaml"))
	if err != nil {
		t.Fatalf("read the example: %v", err)
	}
	for _, key := range []string{"cloud:", "profile:", "store:", "bucket_lookup:", "access_key:"} {
		if !strings.Contains(string(body), key) {
			t.Errorf("the example file names no %s", key)
		}
	}
}
