package resource

// The pause a session declares is one number and two formatters: this package
// writes the length into the page, and ui/assets/js/guided.js counts the same
// seconds down in the browser. Either side can be edited without the other, and
// a few sampled lengths can agree at every value asked while one between them
// does not, so this case asks for every length rather than some: kit/entity's
// validateTiming admits a rest, a target and a timer between 0 and 86400 seconds
// and the rest screen clamps every offer it makes inside that ceiling, so one
// answer per second over the admitted range is every length a person can be
// shown and nothing outside it.

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// declaredCeiling is the longest length kit/entity/guided_validate.go admits for
// a rest between sets, a rest after a step, a target in seconds and a named
// timer; ui/screens/guided.go clamps the seconds a page adds on top of a rest
// inside the same number, so no page can write a length beyond it.
const declaredCeiling = 86400

// TestTheScriptCountsEveryPauseASessionMayDeclare reads the countdown clock out
// of the browser script and requires it to print, for every second count a
// session may declare, the string guidedDuration writes for the same count. A
// clock this case cannot read fails the run rather than skipping: the promise it
// checks only holds while both sides say the length the same way.
func TestTheScriptCountsEveryPauseASessionMayDeclare(t *testing.T) {
	source, err := os.ReadFile("../assets/js/guided.js")
	if err != nil {
		t.Fatalf("no countdown script to read: %v", err)
	}
	text := string(source)
	const head, tail = "const clock = ", "\n};"
	at := strings.Index(text, head)
	if at < 0 {
		t.Fatalf("the countdown script holds no %q this case can read, so the page and the countdown are no longer one promise", head)
	}
	end := strings.Index(text[at:], tail)
	if end < 0 {
		t.Fatalf("the countdown script's clock never closes")
	}
	clock := text[at+len(head) : at+end+len("\n}")] // the arrow function itself, its closing brace and no trailing semicolon
	harness := `let src="";process.stdin.on("data",d=>src+=d);process.stdin.on("end",()=>{` +
		`const clock=eval("("+src+")");` +
		`process.stdout.write(Array.from({length:` + strconv.Itoa(declaredCeiling+1) + `},(_,s)=>clock(s)).join("\n"))});`
	cmd := exec.CommandContext(t.Context(), "node", "-e", harness)
	cmd.Stdin = strings.NewReader(clock)
	var said, silence strings.Builder
	cmd.Stdout, cmd.Stderr = &said, &silence
	if err := cmd.Run(); err != nil {
		t.Fatalf("the countdown clock could not answer for every declared pause: %v\n%s", err, silence.String())
	}
	spoken := strings.Split(said.String(), "\n")
	if len(spoken) != declaredCeiling+1 {
		t.Fatalf("the countdown clock answered %d length(s) for the %d second counts a session may declare", len(spoken), declaredCeiling+1)
	}
	for seconds, line := range spoken {
		if want := guidedDuration(int32(seconds)); line != want {
			t.Fatalf("%d seconds: the page writes %q where the countdown prints %q", seconds, want, line)
		}
	}
}
