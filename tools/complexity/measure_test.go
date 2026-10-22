package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

func measured(t *testing.T, path, text string) measurement {
	t.Helper()
	files := []source{{path, []byte(text)}}
	if !strings.HasPrefix(path, "kit/httpx/") {
		files = append(files, source{"kit/httpx/doc.go", []byte("package httpx\n")})
	}
	m, err := measure(files)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func sizedFunction(name string, lines int) string {
	return "func " + name + "() {\n" + strings.Repeat("\n", lines-2) + "}\n"
}

func requireCount(t *testing.T, m measurement, key string, want int64) {
	t.Helper()
	if m.counts[key] != want {
		t.Fatalf("%s = %d, want %d; offenders %+v", key, m.counts[key], want, m.offenders)
	}
}

func TestFunctionLineThreshold(t *testing.T) {
	m := measured(t, "a.go", "package a\n"+sizedFunction("Hundred", 100)+sizedFunction("Over", 101))
	requireCount(t, m, "funcs_over_100", 1)
	for _, o := range m.offenders {
		if o.measure == "funcs_over_100" && (o.line != 102 || o.size != 101 || o.name != "Over") {
			t.Fatalf("bad position: %+v", o)
		}
	}
	var out bytes.Buffer
	printMeasurement(&out, m, zeroBudget())
	if !strings.Contains(out.String(), "a.go:102 Over size=101 lines") {
		t.Fatal(out.String())
	}
}

func TestComplexityThresholdAndBranches(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{"", 1}, {"if true {} else {}", 2}, {"for {}", 2}, {"for range 3 {}", 2},
		{"switch 1 {case 1,2:; default:}", 2}, {"switch {default:}", 1},
		{"select {case <-make(chan int):; default:}", 2}, {"select {default:}", 1},
		{"_ = true && false || true", 3}, {"_ = !true", 1},
	} {
		file, err := parser.ParseFile(token.NewFileSet(), "a.go", "package a; func f(){"+tc.body+"}", 0)
		if err != nil {
			t.Fatal(err)
		}
		if got := complexity(file.Decls[0].(*ast.FuncDecl).Body); got != tc.want {
			t.Errorf("%q: %d want %d", tc.body, got, tc.want)
		}
	}
	text := "package a\nfunc thirty(){" + strings.Repeat("if true {};", 29) + "}\nfunc over(){" + strings.Repeat("if true {};", 30) + "}\n"
	m := measured(t, "a.go", text)
	requireCount(t, m, "cc_over_30", 1)
	if m.offenders[0].name != "over" || m.offenders[0].size != 31 {
		t.Fatal(m.offenders)
	}
}

func TestNestedLiteralBelongsToContainingFunction(t *testing.T) {
	m := measured(t, "a.go", "package a\ntype Box[A,B any] struct{}\nfunc (b *Box[A,B]) Work(){ _ = func(){"+strings.Repeat("if true {};", 30)+"} }\n")
	requireCount(t, m, "cc_over_30", 1)
	requireCount(t, m, "funcs_over_100", 0)
	if m.offenders[0].name != "Box.Work" {
		t.Fatal(m.offenders)
	}
}

func TestFileLineThreshold(t *testing.T) {
	for _, n := range []int{800, 801} {
		m := measured(t, "a.go", "package a\n"+strings.Repeat("\n", n-1)+"// unterminated")
		want := int64(0)
		if n == 801 {
			want = 1
		}
		requireCount(t, m, "files_over_800", want)
		if want == 1 && (m.offenders[0].size != 801 || m.offenders[0].line != 1 || m.offenders[0].name != "a.go") {
			t.Fatal(m.offenders)
		}
	}
}

func TestCommentGroupThreshold(t *testing.T) {
	for _, n := range []int{14, 15} {
		for _, text := range []string{strings.Repeat("// prose\n", n), "/*\n" + strings.Repeat("prose\n", n-2) + "*/\n"} {
			m := measured(t, "doc.go", "package prose\n"+text)
			want := int64(0)
			if n == 15 {
				want = 1
			}
			requireCount(t, m, "long_comment_blocks", want)
		}
	}
	m := measured(t, "doc.go", "package prose\n"+strings.Repeat("// prose\n", 10)+"\n"+strings.Repeat("// prose\n", 10))
	requireCount(t, m, "long_comment_blocks", 0)
}

func TestHistoryCommentLinePatterns(t *testing.T) {
	positives := []string{"review1", "review 2", "finding3", "round 4", "T-123", "D12", "D-12", "decision 8", "ADR9", "ADR-9", "r5 finding", "CoDeX", "grok", "deepseek", "2026-99-99", "review 9 and round 3"}
	text := "package prose\n// ordinary review and round\nconst s = `review 3 2026-10-01`\n"
	for _, s := range positives {
		text += "// " + s + "\n"
	}
	text += "/* review 1 */ /* round 3 */\n"
	requireCount(t, measured(t, "a.go", text), "history_comment_lines", int64(len(positives)+2))
}

func TestHistoryBlockCommentPositions(t *testing.T) {
	m := measured(t, "physical.go", "package prose\n//line fake.go:900\n/*\nordinary\nreview 7\n*/\n")
	requireCount(t, m, "history_comment_lines", 1)
	if o := m.offenders[0]; o.line != 5 || o.path != "physical.go" {
		t.Fatal(o)
	}
}

func TestPackageStutterScope(t *testing.T) {
	text := `package foo
 type Foo struct {FooField int}
 var FooValue int
 const FOOConst = 1
 func Food() {}
 func FooBodyless()
 func (Foo) FooMethod() {}
 func fooLocal() {}
 `
	requireCount(t, measured(t, "a.go", text), "stutters", 4)
	for _, pkg := range []string{"main", "ab"} {
		requireCount(t, measured(t, "a.go", fmt.Sprintf("package %s\ntype %sThing int\n", pkg, strings.ToUpper(pkg))), "stutters", 0)
	}
}

func TestHTTPDocumentationSurface(t *testing.T) {
	text := `package httpx
const (Alpha=1; Beta=2)
var (First=1; Second=2)
type Kind int
const (One Kind=1; Two Kind=2)
var Typed Kind
type Thing struct{ Field int }
func NewThing() *Thing {return nil}
func Plain() {}
func (*Thing) Method() {}
type Éclair int
`
	m := measured(t, "kit/httpx/a.go", text)
	requireCount(t, m, "httpx_exports", 8)
	want := map[string]int{"Alpha, Beta": 2, "First, Second": 3, "Kind": 4, "One, Two": 5, "Typed": 6, "Thing": 7, "NewThing": 8, "Plain": 9}
	for _, o := range m.offenders {
		if want[o.name] != o.line {
			t.Errorf("unexpected declaration %+v", o)
		}
		delete(want, o.name)
	}
	if len(want) != 0 {
		t.Fatal(want)
	}
	r, _ := fixture(t, 0)
	writeFile(t, r.root, "kit/httpx/a.go", text)
	cmd := exec.CommandContext(t.Context(), "go", "doc", "-short", "./kit/httpx")
	cmd.Dir = r.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go doc: %v %s", err, out)
	}
	count := 0
	pattern := regexp.MustCompile(`^(func|type|var|const) [A-Z]`)
	for line := range strings.SplitSeq(string(out), "\n") {
		if pattern.MatchString(strings.TrimSpace(line)) {
			count++
		}
	}
	if int64(count) != m.counts["httpx_exports"] {
		t.Fatalf("go doc %d vs AST %v: %s", count, m.counts, out)
	}
}

func TestEveryOverBudgetOffenderIsReported(t *testing.T) {
	text := "package a\n"
	for i := range 25 {
		text += sizedFunction(fmt.Sprintf("Over%02d", i), 101)
	}
	m := measured(t, "new\nfile.go", text)
	var out bytes.Buffer
	printMeasurement(&out, m, zeroBudget())
	last := -1
	for i := range 25 {
		needle := fmt.Sprintf("Over%02d size=101 lines", i)
		pos := strings.Index(out.String(), needle)
		if pos <= last {
			t.Fatalf("missing or unsorted %s: %s", needle, out.String())
		}
		last = pos
	}
	if !strings.Contains(out.String(), `new\nfile.go:2`) {
		t.Fatal(out.String())
	}
}
