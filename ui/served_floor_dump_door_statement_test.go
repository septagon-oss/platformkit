package ui_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The door naming where the loop's gate writes is answered by the paths a journey writes its dumps at, so a
// byte that lands outside the directory the caller named leaves the door's promise broken. The check beside
// this one reads the reading file one line at a time, which judges a call by the line its name sits on: a
// target on the following line is then a write at a name no dump door named, although the call writes exactly
// where the door pointed; and a call whose name the check does not list — a descriptor opened with openSync and
// written with writeSync, a Playwright screenshot or PDF saved to a file — puts bytes wherever it likes while
// every dump-door check reports the file clean.
//
// TestEachDumpDoorWriteNamesTheDoorInTheStatementThatMakesIt asks the same promise of a whole call. It joins the
// lines a call spans into the statement that makes it and hands that to the check beside this one, so a target
// counts wherever it sits in the call, and it asks which names the reading file puts bytes on disk through:
// every one of them must be a name that check reads, because one it does not read answers nothing. It keeps
// making directories out, since a directory is not bytes, and shares the per-file limit of the checks beside
// it: a value carried into a helper of its own moves the answer to that helper, and a section documenting no
// dump door leaves this nothing to ask.
func TestEachDumpDoorWriteNamesTheDoorInTheStatementThatMakesIt(t *testing.T) {
	var dumps []string
	for _, door := range servedDoor.FindAllString(servedDesignFloorProse(t), -1) {
		if strings.HasSuffix(door, "_OUT") && !slices.Contains(dumps, door) {
			dumps = append(dumps, door)
		}
	}
	if len(dumps) == 0 {
		t.Log("the served design floor section documents no door naming where the gate's dumps go")
		return
	}
	sort.Strings(dumps)
	judged := 0
	for _, reader := range gatePathReaders(t) {
		source, err := os.ReadFile(reader.path)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("read %s to judge the dump doors it reads: %v", reader.path, err)
			}
			continue
		}
		asked := false
		for _, door := range dumps {
			variable, ok := reader.variables[door]
			if !ok {
				continue
			}
			asked = true
			judged++
			strays, inside := writesAtTheDoor(t, wholeCalls(t, reader.path, string(source)), variable)
			if inside == 0 {
				t.Errorf("%s reads the documented dump door %s into %s and puts no bytes on disk at a name built from it, read one call at a time", reader.path, door, variable)
			}
			for _, statement := range strays {
				t.Errorf("%s reads the documented dump door %s into %s and puts bytes on disk at a name no dump door named: %s", reader.path, door, variable, statement)
			}
		}
		for _, name := range namesThatPutBytesOnDisk(string(source)) {
			if !asked || writesBytes.MatchString(name+"(") {
				continue
			}
			t.Errorf("%s puts bytes on disk through %s, a call the dump-door checks do not read: what it writes answers nothing about where the door points", reader.path, name)
		}
	}
	if judged == 0 {
		t.Logf("no file reads the documented dump door(s) %s, which the check beside this one reports", strings.Join(dumps, ", "))
	}
}

// fsImport names the functions a journey takes from the file system, which is where a new way to put bytes on
// disk arrives: a call listed here is one the reading file uses, and one the dump-door checks must read.
var fsImport = regexp.MustCompile(`(?m)^\s*import\s*\{([^}]*)\}\s*from\s*['"]node:fs(?:/promises)?['"]`)

// savesToAFile names the Playwright calls that write bytes at a path the call itself carries, so a dump is not
// limited to the file-system functions.
var savesToAFile = regexp.MustCompile(`(?m)\b(screenshot|pdf)\s*\(`)

// makesDirectories names what leaves a path without bytes: the checks beside this one answer a dump door at the
// file, and a directory made at the door is their own question.
var makesDirectories = regexp.MustCompile(`^(?:mkdirSync|mkdtempSync|rmSync)$`)

// namesThatPutBytesOnDisk lists, in order, the names one source file puts bytes on disk through: the writers it
// imports from the file system and the browser calls that save to a path.
func namesThatPutBytesOnDisk(source string) []string {
	var names []string
	for _, specifier := range fsImport.FindAllStringSubmatch(source, -1) {
		for _, name := range strings.Split(specifier[1], ",") {
			name = strings.TrimSpace(strings.Split(name, " as ")[0])
			if name == "" || makesDirectories.MatchString(name) {
				continue
			}
			if (strings.Contains(name, "rite") || strings.Contains(name, "ppend") || strings.HasPrefix(name, "createWriteStream")) && !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	for _, save := range savesToAFile.FindAllStringSubmatch(source, -1) {
		if !slices.Contains(names, save[1]) {
			names = append(names, save[1])
		}
	}
	sort.Strings(names)
	return names
}

// templateLiteral matches a JavaScript or TypeScript template string, the one spelling that builds a path out
// of a variable while looking like a literal.
var templateLiteral = regexp.MustCompile("`(?:[^`\\\\]|\\\\.)*`")

// interpolatedName matches what a template string interpolates: an expression between ${ and }.
var interpolatedName = regexp.MustCompile(`\$\{([^{}]*)\}`)

// interpolatedNames replaces one template string with the expressions it interpolates, so a name built into a
// path that way survives the blanking the checks beside this one do to every literal. A template string that
// carries a scheme stays blank: it names something the run reads over HTTP, not a file it writes, and the name
// inside it would otherwise answer for a write somewhere else on the line.
func interpolatedNames(literal string) string {
	if strings.Contains(literal, "://") {
		return ""
	}
	var names []string
	for _, expr := range interpolatedName.FindAllStringSubmatch(literal, -1) {
		names = append(names, expr[1])
	}
	return strings.Join(names, " ")
}

// wholeCalls returns the path of a copy of the source in which each call that puts bytes on disk spans one
// line, so the check beside this one judges that call by everything between its brackets rather than by the
// line its name sits on. Only those calls are joined: every other line keeps its own place, so the assignments
// the check follows to build a path are read as the file wrote them. Template strings keep the expressions they
// interpolate and lose their text, because a path written as `${out}/page.html` is built from the door as
// surely as one passed to join: blanking every literal, which the checks beside this one do to stop a literal
// that spells a variable's name counting as that variable, also hides the variable inside a template string.
func wholeCalls(t *testing.T, path, source string) string {
	t.Helper()
	source = templateLiteral.ReplaceAllStringFunc(source, interpolatedNames)
	lines := strings.Split(source, "\n")
	var joined []string
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		for depth := callDepth(line); writesBytes.MatchString(codeOf(line)) && depth > 0 && i+1 < len(lines); depth = callDepth(line) {
			i++
			line += " " + strings.TrimSpace(lines[i])
		}
		joined = append(joined, line)
	}
	copied := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := os.WriteFile(copied, []byte(strings.Join(joined, "\n")), 0o600); err != nil {
		t.Fatalf("write the joined copy of %s to judge its calls: %v", path, err)
	}
	return copied
}

// codeOf returns one line with its string literals blanked and its comment removed, which is what remains of
// the code the checks beside this one read.
func codeOf(line string) string {
	line = literalText.ReplaceAllString(line, "")
	if note := strings.Index(line, "//"); note >= 0 {
		return line[:note]
	}
	return line
}

// callDepth counts the brackets, braces and parens one line of code leaves open, so a call whose closing
// bracket a formatter moved to a following line can be read as the one call it is.
func callDepth(line string) int {
	depth := 0
	for _, r := range codeOf(line) {
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	return depth
}
