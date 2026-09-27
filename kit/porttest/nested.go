package porttest

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

// markFraming is the control byte Go's testing package writes in front of a
// framing line ("=== RUN   X", "--- FAIL: X (0.00s)") when the binary reports
// in -test.v=test2json mode. cmd/go reads such a line as the framework's own
// word and turns it into an event for a test with that name.
const markFraming = 0x16

// NestedRuns runs the test binary and keeps the nested runs named here out of
// the framework's framing — the only thing that makes a runner's line readable
// as the framework reporting a test.
//
// A mutation proof that runs a whole suite through testing.RunTests asks one
// question of it — did it pass — and testing.RunTests answers by writing the
// framing lines a test runner writes, from a second runner inside the first.
// cmd/go cannot tell the two runners apart, so the suite's cases arrive as
// events of tests the package does not contain: a package whose every test
// passed reports a failing tasktest_RunService, and make check fails on the
// mutant a suite refused on purpose. The names are the mutation proofs' own, and
// none of them can ever be a test of the package — which is now this function's own
// refusal and not the habit of its callers. A name beginning with Test is one a real
// test could report under, and demoting a genuine `--- FAIL: TestSomething` to output
// is how a red package arrives green under make check, so NestedRuns runs nothing and
// returns 1 on such a name. Given no name at all it would buy a pipe to strip nothing
// and returns 1 for that too — both refusals are decided before m.Run, so both are
// reachable without a test binary to run.
//
// A package calls this from its TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(porttest.NestedRuns(m, "tasktest_RunService")) }
func NestedRuns(m *testing.M, names ...string) int {
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "porttest: NestedRuns was given no nested run to strip, so it would move the whole suite through a pipe to change nothing")
		return 1
	}
	for _, name := range names {
		if strings.HasPrefix(name, "Test") {
			fmt.Fprintf(os.Stderr, "porttest: NestedRuns refuses to strip %q: a test function's name begins with Test, so stripping it would demote a real --- FAIL: %s to output and turn a failing package green\n", name, name)
			return 1
		}
	}
	saved := os.Stdout
	reading, writing, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "porttest: NestedRuns cannot open a pipe for %v: %v\n", names, err)
		return 1
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		unnest(reading, saved, names)
	}()
	os.Stdout = writing
	code := m.Run()
	_ = writing.Close()
	<-done
	os.Stdout = saved
	return code
}

// unnest copies r to w, dropping the framing byte from the lines a nested run
// writes and copying every other line unchanged. The text survives — a reader
// of -v output still sees what the mutant did — and only the claim that these
// lines are the framework reporting a test goes away.
func unnest(r io.Reader, w io.Writer, names []string) {
	in, f := bufio.NewReader(r), nestedRun{names: names}
	for {
		line, rerr := in.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(f.strip(line)); werr != nil {
				return // nowhere left to say it
			}
		}
		if rerr != nil {
			return
		}
	}
}

// nestedRun is the state one copy of the stream needs: which nested run, if any,
// is answering now, and whether its report line has gone by so that the bare
// PASS or FAIL it prints as a runner may follow on the very next line.
type nestedRun struct {
	names  []string
	active string
	ends   bool
}

func (n *nestedRun) strip(line []byte) []byte {
	if len(line) == 0 || line[0] != markFraming {
		return line // already the framework's plain output
	}
	body := line[1:]
	if n.ends { // whatever follows the report ends the run, PASS or not
		n.active, n.ends = "", false
		if name, _ := marker(body); name == "" && bareResult(body) {
			return body
		}
	}
	name, report := marker(body)
	switch {
	case n.active == "":
		if name == "" || report || !n.owns(name) {
			return line
		}
		n.active = name // this nested run's output begins here
		return body
	case n.owns(name):
		if report && name == n.active {
			n.ends = true
		}
		return body
	}
	return line
}

// owns reports whether the named frame belongs to the nested run in progress,
// or — before one began — to one of the nested runs this package declared.
func (n *nestedRun) owns(name string) bool {
	if n.active != "" {
		return name == n.active || strings.HasPrefix(name, n.active+"/")
	}
	for _, declared := range n.names {
		if name == declared {
			return true
		}
	}
	return false
}

// marker returns the test a framing line speaks of, and whether it reports a
// finished result rather than opening a frame. An empty name is not a frame at
// all: the line is output, an update or a runner's own PASS or FAIL.
func marker(body []byte) (name string, report bool) {
	b := strings.TrimLeft(string(body), " ")
	for _, p := range []string{"=== RUN   ", "=== PAUSE ", "=== CONT  ", "=== NAME  "} {
		if rest, ok := strings.CutPrefix(b, p); ok {
			return strings.TrimSpace(rest), false
		}
	}
	for _, p := range []string{"--- PASS: ", "--- FAIL: ", "--- SKIP: "} {
		if rest, ok := strings.CutPrefix(b, p); ok {
			name, _, _ := strings.Cut(rest, " ")
			return name, true
		}
	}
	return "", false
}

// bareResult reports the PASS or FAIL a test binary prints when its run is over,
// which a nested runner prints for itself in the middle of the real one.
func bareResult(body []byte) bool {
	t := bytes.TrimSpace(body)
	return bytes.Equal(t, []byte("PASS")) || bytes.Equal(t, []byte("FAIL"))
}
