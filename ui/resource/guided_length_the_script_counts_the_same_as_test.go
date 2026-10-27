package resource

// A pause is written twice, in two languages over one number: this package
// writes the length into the page a person who never runs a script still reads,
// and ui/assets/js/guided.js writes the same length as a countdown. They are two
// implementations of one promise — the line above the countdown is the line the
// countdown keeps — and either can be edited without the other, which is how a
// person reloading mid-pause would read "62:05" and count down "1:02:05".
// Nothing but a shared number joins them, so this case asks the browser's own
// function the questions the page answers.

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestTheScriptCountsTheLengthThePageWrote reads the countdown clock out of the
// browser script, lets node answer for the seconds a session can declare — the
// short pause, the hour both sides switch shape at, and the day the entity stops
// at — and requires each answer to be the string guidedDuration writes for the
// same number. A clock this case cannot read is a failure, not a skip: the
// promise it checks only exists while both sides say the length the same way.
func TestTheScriptCountsTheLengthThePageWrote(t *testing.T) {
	const declared = "const clock = "
	source, err := os.ReadFile("../assets/js/guided.js")
	if err != nil {
		t.Fatalf("no countdown script to read: %v", err)
	}
	text := string(source)
	at := strings.Index(text, declared)
	if at < 0 {
		t.Fatalf("the countdown script holds no %q this case can read, so the page and the clock are no longer one promise", declared)
	}
	end := strings.Index(text[at:], "\n};")
	if end < 0 {
		t.Fatalf("the countdown script's clock never closes")
	}
	clock := text[at+len(declared) : at+end+len("\n}")] // the arrow function itself, its closing brace and no trailing semicolon
	seconds := []int32{0, 1, 59, 60, 61, 3599, 3600, 3661, 3725, 45296, 86400}
	harness := `let src="";process.stdin.on("data",d=>src+=d);process.stdin.on("end",()=>{` +
		`const clock=eval("("+src+")");` +
		`process.stdout.write(process.argv.slice(1).map(n=>clock(Number(n))).join("\n"))});`
	args := []string{"-e", harness}
	for _, s := range seconds {
		args = append(args, strconv.Itoa(int(s)))
	}
	cmd := exec.CommandContext(t.Context(), "node", args...)
	cmd.Stdin = strings.NewReader(clock)
	var said, silence strings.Builder
	cmd.Stdout, cmd.Stderr = &said, &silence
	if err := cmd.Run(); err != nil {
		t.Fatalf("the countdown clock could not answer: %v\n%s", err, silence.String())
	}
	spoken := strings.Split(strings.TrimRight(said.String(), "\n"), "\n")
	if len(spoken) != len(seconds) {
		t.Fatalf("the countdown clock answered %d length(s) for %d second count(s)", len(spoken), len(seconds))
	}
	for i, line := range spoken {
		if want := guidedDuration(seconds[i]); line != want {
			t.Errorf("%d seconds: the page writes %q where the countdown prints %q", seconds[i], want, line)
		}
	}
}
