package main

// start_preflight.go answers, before a run spends anything, the questions a
// machine can answer about itself: is this a machine `start` undertakes to
// answer for, is the address free, are the two directories usable, and does the
// data directory already belong to something — a running database, or a database
// this command would otherwise replace.
//
// Every refusal here names the value it refused, the flag or path that would
// change it, and what the run did not do: write nothing, download nothing. A
// refusal never drives a process or a port it did not start, which is the rule
// scripts/free_port.sh already states for the shell.

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// supportedTargets are the machines `start` undertakes to answer for, as
// GOOS/GOARCH. A pair outside this list is refused by name rather than tried:
// the embedded database is downloaded per pair and the run cannot know the
// answer by attempting it.
//
// WSL is not a row: inside WSL2 the runtime answers linux/amd64 or linux/arm64,
// so it takes the Linux answer by construction and nothing here branches on it.
// Windows is refused because nothing here has run it — see the sentence that
// refusal prints.
var supportedTargets = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64"}

// databaseMajor is the major version this command runs, read from the pinned
// dependency rather than written down again: a cluster whose PG_VERSION names
// anything else is a cluster this command must not touch.
func databaseMajor() string {
	return strings.SplitN(string(embeddedVersion), ".", 2)[0]
}

func databaseVersionMatches(found string) bool {
	return strings.TrimSpace(found) == databaseMajor()
}

func supportedTarget(goos, goarch string) bool {
	for _, target := range supportedTargets {
		if target == goos+"/"+goarch {
			return true
		}
	}
	return false
}

// launch is where the database goes and which port it is told to use. The
// preflight decides the two directories; the launcher is only ever told the answer.
type launch struct {
	data  string
	cache string
	port  int
}

// cluster is the database directory inside the data directory — the directory the
// launcher would delete if it did not recognise it, which is why the preflight
// asks about it first.
func (l launch) cluster() string { return filepath.Join(l.data, "postgres") }

// database is the launcher's surface as this command uses it: bring it up, and
// bring it down again on the way out.
type database interface {
	Start() error
	Stop() error
}

// startDeps are the externally-visible decisions startApp makes, held so a test
// can make them itself. The zero value is the production answer.
type startDeps struct {
	machine  func() (goos, goarch string)
	listen   func(network, address string) (net.Listener, error)
	cacheDir func() (string, error)
	launchDB func(launch) (database, error)
}

func (d startDeps) withDefaults() startDeps {
	if d.machine == nil {
		d.machine = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	}
	if d.listen == nil {
		d.listen = net.Listen
	}
	if d.cacheDir == nil {
		d.cacheDir = os.UserCacheDir
	}
	if d.launchDB == nil {
		d.launchDB = startPostgres
	}
	return d
}

// startup is what the preflight settled: where the run will write, on which
// address. The held listener keeps that address until the application binds it.
type startup struct {
	launch
	addr string
	hold net.Listener
}

func (s startup) release() {
	if s.hold != nil {
		_ = s.hold.Close()
	}
}

// preflight runs the checks in the order their cost rises: the machine, the
// address, the two directories, then what the data directory already holds. It
// writes nothing outside the directories it is checking and reaches no network.
func (d startDeps) preflight(data, addr string) (startup, error) {
	goos, goarch := d.machine()
	if !supportedTarget(goos, goarch) {
		return startup{}, errUnsupportedMachine(goos, goarch)
	}
	hold, err := d.bindTarget(addr)
	if err != nil {
		return startup{}, err
	}
	run := startup{addr: addr, hold: hold}
	if err := run.settle(d, data); err != nil {
		run.release()
		return startup{}, err
	}
	return run, nil
}

// bindTarget asks whether the address is bindable by binding it, and keeps the
// socket so nothing can take the address while the run goes about its work. A
// loss of the race after this point is answered by the application's own bind,
// which is why that refusal says the same thing again.
func (d startDeps) bindTarget(addr string) (net.Listener, error) {
	hold, err := d.listen("tcp", addr)
	if err != nil {
		if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
			return nil, fmt.Errorf("start: the address %s needs a privilege this process does not have: %w\n"+
				"       nothing was written and no database was downloaded;\n"+
				"       choose --addr on a port above 1024, for example --addr 127.0.0.1:8080, and rerun", addr, err)
		}
		return nil, fmt.Errorf("start: %s is already in use: %w\n"+
			"       nothing was written and no database was downloaded;\n"+
			"       choose another address with --addr, for example --addr 127.0.0.1:8081, or stop whatever\n"+
			"       answers it, and rerun", addr, err)
	}
	return hold, nil
}

// settle resolves the two directories and then asks the data directory what it
// already holds. Both directories are created if absent: a directory that cannot
// be created is the answer the person needs before the download, not after it.
func (s *startup) settle(d startDeps, data string) error {
	abs, err := filepath.Abs(data)
	if err != nil {
		return fmt.Errorf("start: %s: %w", data, err)
	}
	if err := usableDirectory(abs, "--data"); err != nil {
		return err
	}
	s.data = abs
	if s.cache, err = d.cacheDirectory(); err != nil {
		return err
	}
	return directoryHoldsItsOwnDatabase(s.data, s.cache)
}

// usableDirectory makes dir and proves it takes a file. Why it cannot is said
// with the path and the operating system's own reason, because both of those are
// what the person can act on.
func usableDirectory(dir, flag string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("start: the data directory %s cannot be created: %w\n"+
			"       nothing was written and no database was downloaded;\n"+
			"       rerun with %s pointing somewhere you may write", dir, err, flag)
	}
	probe, err := os.CreateTemp(dir, "platformkit-writable-")
	if err != nil {
		return fmt.Errorf("start: the data directory %s is not writable: %w\n"+
			"       the probe file this check wrote is removed and no database was downloaded;\n"+
			"       rerun with %s pointing somewhere you may write", dir, err, flag)
	}
	name := probe.Name()
	_ = probe.Close()
	return os.Remove(name)
}

// cacheDirectory is where the database binaries live, per user. The first start
// downloads them, so a cache that cannot be written is worth refusing before the
// download rather than a third of the way through it.
func (d startDeps) cacheDirectory() (string, error) {
	base, err := d.cacheDir()
	if err != nil {
		return "", fmt.Errorf("start: this machine offers no cache directory to hold the database binaries: %w\n"+
			"       set %s to a directory you may write and rerun", err, cacheDirVariable())
	}
	cache := filepath.Join(base, "platformkit", "postgres")
	for _, dir := range []string{filepath.Join(cache, "archive"), filepath.Join(cache, "runtime")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("start: the cache directory %s cannot be created: %w\n"+
				"       no database was downloaded and nothing was written under --data;\n"+
				"       set %s somewhere you may write and rerun", dir, err, cacheDirVariable())
		}
	}
	probe, err := os.CreateTemp(filepath.Join(cache, "archive"), "platformkit-writable-")
	if err != nil {
		return "", fmt.Errorf("start: the cache directory %s is not writable: %w\n"+
			"       no database was downloaded and nothing was written under --data;\n"+
			"       set %s somewhere you may write and rerun", cache, err, cacheDirVariable())
	}
	name := probe.Name()
	_ = probe.Close()
	return cache, os.Remove(name)
}

// cacheDirVariable names the setting that moves the cache on the machine this is
// running on. Only Linux is refused by a variable; on macOS the cache sits under
// $HOME and there is nothing to set but the home itself.
func cacheDirVariable() string {
	if runtime.GOOS == "darwin" {
		return "$HOME/Library/Caches"
	}
	return "XDG_CACHE_HOME"
}

// directoryHoldsItsOwnDatabase refuses the two data directories this command must
// not touch: one a live postmaster has open, and one whose contents the embedded
// launcher would delete to make room for a new cluster.
//
// The launcher decides reuse from PG_VERSION alone and, when that file does not
// name its own version, removes the directory and runs initdb — so a cluster
// from another major version, or a directory that is somebody else's, is gone in
// a run that reports success. Asking first is the only answer available to the
// caller that owns the path.
func directoryHoldsItsOwnDatabase(data, cache string) error {
	cluster := launch{data: data}.cluster()
	if err := clusterIsThisVersion(cluster); err != nil {
		return err
	}
	return postmasterHolds(cluster, cache)
}

// clusterIsThisVersion refuses a directory that is not this command's to
// initialise: one whose PG_VERSION names another version, or one that holds
// anything at all and names no version. An absent or empty directory proceeds.
func clusterIsThisVersion(cluster string) error {
	major := databaseMajor()
	content, err := os.ReadFile(filepath.Join(cluster, "PG_VERSION"))
	switch {
	case err == nil:
		if !databaseVersionMatches(string(content)) {
			found := strings.TrimSpace(string(content))
			return fmt.Errorf("start: %s holds a PostgreSQL %s cluster and this command runs %s\n"+
				"       nothing was deleted and nothing was downloaded;\n"+
				"       rerun with --data pointing at an empty directory, or upgrade that cluster yourself",
				cluster, found, major)
		}
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return unknownCluster(cluster, major)
	default:
		return fmt.Errorf("start: %s: %w", filepath.Join(cluster, "PG_VERSION"), err)
	}
}

// unknownCluster is the half of the guard that has no version to compare: a
// directory that is not empty and does not say what it is. Whatever it is, this
// command is about to be asked to delete it, so it refuses instead.
func unknownCluster(cluster, major string) error {
	entries, err := os.ReadDir(cluster)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("start: %s: %w", cluster, err)
	}
	if len(entries) == 0 {
		return nil
	}
	return fmt.Errorf("start: %s holds %d file(s) and no PG_VERSION, so it is not a PostgreSQL %s cluster this command can reuse\n"+
		"       nothing was deleted and nothing was downloaded;\n"+
		"       rerun with --data pointing at an empty directory", cluster, len(entries), major)
}

// postmasterHolds refuses to share a data directory with a postmaster this run
// did not start. A killed run leaves one: the launcher daemonises the server with
// pg_ctl, so it outlives the process that started it, and a second run that got
// as far as initdb would be answered by the operating system, not by this command.
//
// The remedy is printed and never run by us: stopping somebody else's database is
// that process's owner's decision.
func postmasterHolds(cluster, cache string) error {
	content, err := os.ReadFile(filepath.Join(cluster, "postmaster.pid"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("start: %s: %w", filepath.Join(cluster, "postmaster.pid"), err)
	}
	pid := postmasterPID(string(content))
	if pid <= 0 || !processAlive(pid) {
		return nil
	}
	return fmt.Errorf("a PostgreSQL server this run did not start holds %s (pid %d)\n"+
		"       nothing was deleted, no database was downloaded and no server was stopped;\n"+
		"       stop it with %s stop -D %s, then rerun;\n"+
		"       or rerun with --data pointing at another directory — this command never stops a server it did not start",
		cluster, pid, pgCtl(cache), cluster)
}

// postmasterPID is the first line of postmaster.pid, which is the pid; the file's
// second line is the data directory it belongs to, which is why the file read is
// of the one inside the directory being asked about.
func postmasterPID(content string) int {
	line, _, _ := strings.Cut(strings.TrimSpace(content), "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		return 0
	}
	return pid
}

// processAlive asks whether a process exists, with the signal that asks for
// nothing. It never sends a signal anybody could act on.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func errUnsupportedMachine(goos, goarch string) error {
	return fmt.Errorf("start: %s/%s is not a supported machine; this command runs %s\n"+
		"       nothing was written and no database was downloaded;\n"+
		"       on Windows, run `platformkit run` against a PostgreSQL of your own instead —\n"+
		"       nothing undertakes to answer for %s/%s until a release has run it",
		goos, goarch, strings.Join(supportedTargets, ", "), goos, goarch)
}
