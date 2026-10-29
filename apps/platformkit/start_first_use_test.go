package main

// The first-use path's cases: the refusals a stranger's machine reaches, and the
// order they are reached in. The database launcher is the seam — a run that gets
// as far as it reports errLauncherReached — so every "refused first" below is an
// assertion about the order of the run rather than about a second copy of it.

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// errLauncherReached is what the case's launcher answers so a run that reached it
// can be told apart from one that refused first.
var errLauncherReached = errors.New("the case's database launcher was reached")

// attemptStart runs the command with the case's machine, cache and launcher, and
// reports the error and how many times the launcher was reached.
func attemptStart(t *testing.T, data, addr string, d startDeps) (error, int) {
	t.Helper()
	launches := 0
	if d.launchDB == nil {
		d.launchDB = func(launch) (database, error) {
			launches++
			return nil, errLauncherReached
		}
	}
	return startApp([]string{"--data", data, "--addr", addr}, d), launches
}

// A machine this command does not undertake to answer for is refused from the
// pair alone: nothing is downloaded, nothing is written, and the sentence names
// the pair, the rows that are supported and what to do instead.
func TestAMachineOutsideTheSupportedRowsIsRefusedBeforeAnythingIsSpent(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"windows", "amd64"}, {"linux", "386"}, {"linux", "arm"}, {"linux", "ppc64le"}, {"freebsd", "amd64"},
	} {
		goos, goarch := pair[0], pair[1]
		t.Run(goos+"/"+goarch, func(t *testing.T) {
			t.Parallel()
			data := filepath.Join(t.TempDir(), "data")
			err, launches := attemptStart(t, data, "127.0.0.1:0", startDeps{
				machine: func() (string, string) { return goos, goarch },
			})
			if err == nil {
				t.Fatalf("%s/%s was accepted", goos, goarch)
			}
			for _, want := range []string{goos + "/" + goarch, "is not a supported machine", "`platformkit run`"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name %q:\n%v", want, err)
				}
			}
			saysWhatItDidNotDo(t, err)
			for _, row := range supportedTargets {
				if !strings.Contains(err.Error(), row) {
					t.Errorf("the refusal does not list the supported row %s:\n%v", row, err)
				}
			}
			if launches != 0 {
				t.Errorf("the launcher ran %d time(s) on a refused machine", launches)
			}
			if _, err := os.Stat(data); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the refused run left %s behind (err %v)", data, err)
			}
		})
	}
}

// The guard above is a list of four rows, not a blocklist: every supported pair has
// to get past it, whatever else it says.
func TestEverySupportedRowReachesTheDatabase(t *testing.T) {
	t.Parallel()
	for _, row := range supportedTargets {
		goos, goarch, _ := strings.Cut(row, "/")
		t.Run(row, func(t *testing.T) {
			t.Parallel()
			err, launches := attemptStart(t, filepath.Join(t.TempDir(), "data"), "127.0.0.1:0", startDeps{
				machine: func() (string, string) { return goos, goarch },
			})
			if launches != 1 {
				t.Fatalf("%s reached the launcher %d time(s), err %v", row, launches, err)
			}
			if !errors.Is(err, errLauncherReached) {
				t.Errorf("%s was refused before the database: %v", row, err)
			}
		})
	}
}

// An occupied address is the commonest way a second run goes wrong, and it used to
// be found last, after the download, the migration and the printed password.
func TestAnOccupiedAddressIsRefusedBeforeTheDatabaseIsReached(t *testing.T) {
	t.Parallel()
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	refused, launches := attemptStart(t, t.TempDir(), held.Addr().String(), startDeps{})
	if refused == nil {
		t.Fatal("an occupied address was accepted")
	}
	if launches != 0 {
		t.Errorf("the database was reached %d time(s) behind an occupied address", launches)
	}
	for _, want := range []string{"--addr", "already in use", "nothing was written"} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
}

// The listener the run did not start is still there afterwards, in the same state:
// a refusal never drives somebody else's socket.
func TestAnOccupiedAddressLeavesTheListenerItFoundAlone(t *testing.T) {
	t.Parallel()
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	accepted := make(chan struct{})
	go func() {
		conn, err := held.Accept()
		if err == nil {
			_ = conn.Close()
			close(accepted)
		}
	}()

	if refused, _ := attemptStart(t, t.TempDir(), held.Addr().String(), startDeps{}); refused == nil {
		t.Fatal("an occupied address was accepted")
	}
	conn, err := net.Dial("tcp", held.Addr().String())
	if err != nil {
		t.Fatalf("the run left the address it refused unlistenable: %v", err)
	}
	_ = conn.Close()
	<-accepted
}

// A port below 1024 is refused for a different reason and needs a different
// sentence: it is the flag that moves, not the other process.
func TestAnAddressThatNeedsPrivilegeNamesTheFlagThatMovesIt(t *testing.T) {
	t.Parallel()
	denied := fmt.Errorf("listen tcp :80: bind: %w", syscall.EACCES)
	refused, launches := attemptStart(t, t.TempDir(), ":80", startDeps{
		listen: func(string, string) (net.Listener, error) { return nil, denied },
	})
	if refused == nil {
		t.Fatal("an address needing privilege was accepted")
	}
	if launches != 0 {
		t.Errorf("the database was reached %d time(s) behind a refused bind", launches)
	}
	for _, want := range []string{"privilege", "--addr"} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
}

func TestAnUnwritableDataDirectoryIsNamedWithItsPath(t *testing.T) {
	t.Parallel()
	readonly := t.TempDir()
	if err := os.Chmod(readonly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o700) })
	data := filepath.Join(readonly, "data")

	refused, launches := attemptStart(t, data, "127.0.0.1:0", startDeps{})
	if refused == nil {
		t.Fatalf("an unwritable --data was accepted")
	}
	if launches != 0 {
		t.Errorf("the database was reached %d time(s) behind an unwritable --data", launches)
	}
	for _, want := range []string{data, "--data"} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
}

// The cache is where the first start downloads to, so the variable that moves it
// belongs in the refusal. XDG_CACHE_HOME is the setting Linux reads.
func TestAnUnwritableCacheDirectoryNamesTheVariableThatMovesIt(t *testing.T) {
	readonly := t.TempDir()
	if err := os.Chmod(readonly, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o700) })
	t.Setenv("XDG_CACHE_HOME", readonly)

	refused, launches := attemptStart(t, t.TempDir(), "127.0.0.1:0", startDeps{})
	if refused == nil {
		t.Fatal("an unwritable cache directory was accepted")
	}
	if launches != 0 {
		t.Errorf("the database was reached %d time(s) behind an unwritable cache", launches)
	}
	for _, want := range []string{readonly, "XDG_CACHE_HOME"} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
}

// The download is the first start's one external dependency. A machine that cannot
// reach it is told which host, that a rerun retries, and where the files go.
func TestADeniedDownloadSaysTheFirstStartNeedsInternet(t *testing.T) {
	t.Parallel()
	cacheRoot := t.TempDir()
	cache := filepath.Join(cacheRoot, "platformkit", "postgres")
	denied := errors.New("unable to connect to https://repo1.maven.org/maven2")

	refused, _ := attemptStart(t, t.TempDir(), "127.0.0.1:0", startDeps{
		cacheDir: func() (string, error) { return cacheRoot, nil },
		launchDB: func(launch) (database, error) { return nil, denied },
	})
	if refused == nil {
		t.Fatal("a denied download was accepted")
	}
	for _, want := range []string{"repo1.maven.org", string(embeddedVersion), "rerun", cache} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
}

// A killed run leaves the server it started: the launcher daemonises it, so it is
// not a child of this process. The next run refuses to share the directory and
// prints the command that stops the server — and never runs it.
func TestADataDirectoryHeldByAnotherServerIsRefusedAndThatServerIsLeftAlone(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	cluster := filepath.Join(data, "postgres")
	if err := os.MkdirAll(cluster, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cluster, "PG_VERSION"), databaseMajor()+"\n")

	holder := exec.Command("sleep", "120")
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = holder.Process.Kill()
		_ = holder.Wait()
	})
	writeFile(t, filepath.Join(cluster, "postmaster.pid"),
		strconv.Itoa(holder.Process.Pid)+"\n"+cluster+"\n")

	refused, launches := attemptStart(t, data, "127.0.0.1:0", startDeps{
		cacheDir: func() (string, error) { return filepath.Join(data, "cache"), nil },
	})
	if refused == nil {
		t.Fatal("a data directory held by a live server was accepted")
	}
	if launches != 0 {
		t.Errorf("the database was reached %d time(s) behind a held data directory", launches)
	}
	for _, want := range []string{strconv.Itoa(holder.Process.Pid), "stop -D " + cluster, "--data"} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
	if err := syscall.Kill(holder.Process.Pid, 0); err != nil {
		t.Errorf("the refusal disturbed the server it refused to share with: %v", err)
	}
}

// A cluster of another major version is the case the launcher would otherwise
// resolve by deleting it: PG_VERSION is the only file it asks.
func TestAnOlderClusterIsRefusedAndLeftIntact(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	cluster := filepath.Join(data, "postgres")
	if err := os.MkdirAll(cluster, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cluster, "PG_VERSION"), "15\n")
	writeFile(t, filepath.Join(cluster, "sentinel"), "the only copy")

	refused, launches := attemptStart(t, data, "127.0.0.1:0", startDeps{})
	if refused == nil {
		t.Fatal("a PostgreSQL 15 cluster was accepted for replacement")
	}
	if launches != 0 {
		t.Errorf("the database was reached %d time(s) behind an older cluster", launches)
	}
	for _, want := range []string{"15", cluster, "--data"} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
	kept, err := os.ReadFile(filepath.Join(cluster, "sentinel"))
	if err != nil || string(kept) != "the only copy" {
		t.Errorf("the refusal did not leave the cluster alone: %q (err %v)", kept, err)
	}
}

// A directory that holds something and names no version is refused for the same
// reason: the only thing the launcher would do with it is remove it.
func TestAForeignPostgresDirectoryIsRefusedAndNotEmptied(t *testing.T) {
	t.Parallel()
	data := t.TempDir()
	cluster := filepath.Join(data, "postgres")
	if err := os.MkdirAll(cluster, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cluster, "somebody-else"), "restored copy")

	refused, launches := attemptStart(t, data, "127.0.0.1:0", startDeps{})
	if refused == nil {
		t.Fatal("a directory that is not a cluster of this version was accepted for replacement")
	}
	if launches != 0 {
		t.Errorf("the database was reached %d time(s) behind a foreign directory", launches)
	}
	for _, want := range []string{cluster, "--data"} {
		if !strings.Contains(refused.Error(), want) {
			t.Errorf("the refusal does not name %q:\n%v", want, refused)
		}
	}
	saysWhatItDidNotDo(t, refused)
	if _, err := os.ReadFile(filepath.Join(cluster, "somebody-else")); err != nil {
		t.Errorf("the refusal did not leave the directory alone: %v", err)
	}
}

// The guard above must not become a refusal of every rerun: nothing there, nothing
// but directories, and a cluster of this version all reach the database.
func TestDataDirectoryStatesThatStillReachTheDatabase(t *testing.T) {
	t.Parallel()
	for _, state := range []struct {
		name    string
		prepare func(t *testing.T, cluster string)
	}{
		{"absent", func(*testing.T, string) {}},
		{"empty", func(t *testing.T, cluster string) {
			if err := os.MkdirAll(cluster, 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"this version", func(t *testing.T, cluster string) {
			if err := os.MkdirAll(cluster, 0o700); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(cluster, "PG_VERSION"), databaseMajor()+"\n")
		}},
	} {
		t.Run(state.name, func(t *testing.T) {
			t.Parallel()
			data := t.TempDir()
			state.prepare(t, filepath.Join(data, "postgres"))
			err, launches := attemptStart(t, data, "127.0.0.1:0", startDeps{})
			if launches != 1 || !errors.Is(err, errLauncherReached) {
				t.Fatalf("a %s data directory did not reach the database: launches %d, err %v", state.name, launches, err)
			}
		})
	}
}

// A rerun creates no administrator, so it prints no password: the value exists
// exactly once, on the run that made it.
func TestTheRerunBannerPrintsNoPassword(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	firstUse{reference: "PlatformKit test", data: "/home/someone/platformkit",
		cluster: "/home/someone/platformkit/postgres", cache: "/c",
		public: "platformkit.localhost:8080"}.write(&out)
	text := out.String()
	if strings.Contains(text, "password: ") {
		t.Errorf("the rerun banner prints a password:\n%s", text)
	}
	if !strings.Contains(text, "the first run printed") {
		t.Errorf("the rerun banner does not say where the password is:\n%s", text)
	}
	if !strings.Contains(text, "/home/someone/platformkit") {
		t.Errorf("the rerun banner does not name the data directory it reuses:\n%s", text)
	}
}

// Every address the banner prints is the one this run listens on: a stranger who
// types what they were told reaches this application, and the tenant is identified
// by that host.
func TestTheBannerAdvertisesTheAddressThisRunListensOn(t *testing.T) {
	t.Parallel()
	cfg := startConfig("data", "127.0.0.1:9095", 5432)
	var out bytes.Buffer
	banner := firstUse{reference: "PlatformKit test", data: "/d", cluster: "/d/postgres",
		cache: "/c", public: cfg.Server.PublicHost, password: "printed once"}
	banner.write(&out)

	text := out.String()
	lines := 0
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, "http") {
			continue
		}
		lines++
		if !strings.Contains(line, cfg.Server.PublicHost) {
			t.Errorf("a URL the banner prints does not carry the address this run bound:\n%s", line)
		}
	}
	if lines < 5 {
		t.Fatalf("the banner advertises %d URLs, want the site, the sign-in page, the API and the two commands:\n%s", lines, text)
	}
	for _, want := range []string{"/app/admin/login", "/api/v1/auth/login", "/api/v1/task/tasks", "Ctrl-C"} {
		if !strings.Contains(text, want) {
			t.Errorf("the banner does not name %q:\n%s", want, text)
		}
	}
}

// The first run prints a password and the harness captures everything it printed.
// What is kept is the transcript of that run through the redactor, and a redaction
// that missed the password would publish it.
func TestTheCapturedFirstRunCarriesNoCredential(t *testing.T) {
	t.Parallel()
	password, err := generatePassword()
	if err != nil {
		t.Fatal(err)
	}
	var banner bytes.Buffer
	firstUse{reference: "PlatformKit test", data: "/d", cluster: "/d/postgres", cache: "/c",
		public: "platformkit.localhost:8080", password: password}.write(&banner)
	capture := banner.String() +
		"\n$ curl -d '{\"email\":\"admin@platformkit.localhost\",\"password\":\"" + password + "\"}'\n" +
		"start: postgres: dial postgres://platformkit_app:" + password + "@127.0.0.1:5432/platformkit\n"

	redacted := redactForKeeping(t, capture)
	if strings.Contains(redacted, password) {
		t.Errorf("the captured run still carries the password it printed:\n%s", redacted)
	}
	if !strings.Contains(redacted, "[redacted]") {
		t.Errorf("the capture came back unredacted:\n%s", redacted)
	}
	for _, keep := range []string{"platformkit.localhost:8080", "/api/v1/task/tasks"} {
		if !strings.Contains(redacted, keep) {
			t.Errorf("the redaction removed what a receipt has to keep (%s):\n%s", keep, redacted)
		}
	}
}

// redactForKeeping runs the shell's redactor over a capture, so the rule this case
// checks is the one the timing harness uses rather than a copy of it here.
func redactForKeeping(t *testing.T, capture string) string {
	t.Helper()
	command := exec.Command(filepath.Join("..", "..", "scripts", "redact_credentials.sh"))
	command.Stdin = strings.NewReader(capture)
	var out, failures bytes.Buffer
	command.Stdout, command.Stderr = &out, &failures
	if err := command.Run(); err != nil {
		t.Fatalf("the redactor failed: %v\n%s\n%s", err, out.String(), failures.String())
	}
	return out.String()
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// didNotDo are the ways a refusal can state, truthfully, what it left undone: a run
// refused at the bind wrote nothing, one refused at a cluster of another version
// deleted nothing, one refused by the launcher started no tenant. Each case above
// asks its own refusal for one of them, because a refusal that states none leaves a
// person asking whether the directory they pointed at is still theirs.
var didNotDo = []string{
	"nothing was written", "nothing was deleted", "no database was downloaded",
	"no server was stopped", "no tenant was created", "probe file this check wrote is removed",
}

func saysWhatItDidNotDo(t *testing.T, refused error) {
	t.Helper()
	for _, stated := range didNotDo {
		if strings.Contains(refused.Error(), stated) {
			return
		}
	}
	t.Errorf("the refusal states none of what it did not do (%s):\n%v", strings.Join(didNotDo, ", "), refused)
}

// The bind the preflight won can still be lost: the run hands the address back
// seconds before the application binds it. That refusal is the one a run reaches
// after it built the installation, so it says the installation stands and that
// rerunning reuses it — and it is the only error of Run this rewords.
func TestTheBindLostAfterThePreflightSaysTheInstallationStands(t *testing.T) {
	t.Parallel()
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	_, refused := net.Listen("tcp", held.Addr().String())
	if !errors.Is(refused, syscall.EADDRINUSE) {
		t.Fatalf("taking %s back produced %v, not a refused bind", held.Addr(), refused)
	}

	said := lostBind(fmt.Errorf("app: serve: %w", refused),
		startup{launch: launch{data: "/home/someone/platformkit"}, addr: held.Addr().String()})
	for _, want := range []string{held.Addr().String(), "--addr", "/home/someone/platformkit", "rerun"} {
		if !strings.Contains(said.Error(), want) {
			t.Errorf("the refusal does not say %q:\n%v", want, said)
		}
	}
	saysWhatItDidNotDo(t, said)
	if unrelated := errors.New("app: migrations: exit status 1"); lostBind(unrelated, startup{}) != unrelated {
		t.Errorf("a failure that is not the bind came back reworded: %v", lostBind(unrelated, startup{}))
	}
}
