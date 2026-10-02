package dbtest_test

import (
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"testing"

	"gopkg.in/yaml.v3"
)

// The two variables URLs reads are written down twice: once by whoever runs the
// suite on a workstation, and once per job in every workflow that runs it. A job
// that hands the suite a URL with no role, no password or no host does not skip a
// case — URLs fails every case that opens a database, and the job goes red for a
// reason that says nothing about the change it was checking. So every job of
// every workflow that names either variable names a URL this package can open:
// a role, a password, a host and a numeric port, and the app URL points at the
// same server the admin URL does.
func TestEveryWorkflowJobHandsTheSuiteADatabaseURLItCanOpen(t *testing.T) {
	var files []string
	for _, dir := range []string{"../../../.gitea/workflows", "../../../.github/workflows"} {
		matches, err := filepath.Glob(filepath.Join(dir, "*.yml"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	sort.Strings(files)
	checked := 0
	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var workflow struct {
			Env  map[string]string `yaml:"env"`
			Jobs map[string]struct {
				Env map[string]string `yaml:"env"`
			} `yaml:"jobs"`
		}
		if err := yaml.Unmarshal(body, &workflow); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for name, job := range workflow.Jobs {
			env := map[string]string{}
			maps.Copy(env, workflow.Env)
			maps.Copy(env, job.Env)
			admin, hasAdmin := env["PLATFORMKIT_TEST_ADMIN_URL"]
			app, hasApp := env["PLATFORMKIT_TEST_DATABASE_URL"]
			if !hasAdmin && !hasApp {
				continue
			}
			checked++
			where := filepath.Base(path) + " job " + name
			if hasAdmin != hasApp {
				t.Errorf("%s names one of PLATFORMKIT_TEST_ADMIN_URL and PLATFORMKIT_TEST_DATABASE_URL without the other", where)
				continue
			}
			adminHost := openable(t, where, "PLATFORMKIT_TEST_ADMIN_URL", admin)
			appHost := openable(t, where, "PLATFORMKIT_TEST_DATABASE_URL", app)
			if adminHost != "" && appHost != "" && adminHost != appHost {
				t.Errorf("%s: the admin URL reaches %s and the app URL %s; the schema URLs creates is on one server", where, adminHost, appHost)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no workflow job names PLATFORMKIT_TEST_ADMIN_URL, which is not a fact about this repository")
	}
}

// openable reports what in raw would stop URLs from opening it, and returns the
// host:port it reaches when nothing does.
func openable(t *testing.T, where, variable, raw string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Errorf("%s: %s=%q is not a URL: %v", where, variable, raw, err)
		return ""
	}
	if u.User == nil || u.User.Username() == "" {
		t.Errorf("%s: %s=%q names no role", where, variable, raw)
		return ""
	}
	if _, ok := u.User.Password(); !ok {
		t.Errorf("%s: %s=%q carries no password", where, variable, raw)
		return ""
	}
	if u.Hostname() == "" {
		t.Errorf("%s: %s=%q names no host", where, variable, raw)
		return ""
	}
	if _, err := strconv.Atoi(u.Port()); err != nil {
		t.Errorf("%s: %s=%q names no numeric port", where, variable, raw)
		return ""
	}
	return u.Host
}
