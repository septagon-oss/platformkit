package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// longBlock is the length, in lines, at which a run of comment lines is
// reported as a trend. It never fails the check.
const longBlock = 15

// historyDirective, written on the line above a comment, lets one reviewed
// comment keep a refused citation; its reason is printed on every run.
const historyDirective = "//lint:comment-history"

// pattern is one refused citation. Matching is per comment line.
type pattern struct {
	id string
	re *regexp.Regexp
}

// patterns are the citations a production comment never carries: they name
// where a rule came from instead of what it is, and the history is in git.
var patterns = []pattern{
	{"review-number", regexp.MustCompile(`\b[Rr]eview \d+`)},
	{"review-possessive", regexp.MustCompile(`\b[Rr]eview's\b`)},
	{"reviewer-finding", regexp.MustCompile(`\b[Rr]eviewer's finding`)},
	{"round", regexp.MustCompile(`\b[Rr]ound \d+`)},
	{"finding", regexp.MustCompile(`\b[Ff]inding \d+`)},
	{"task-id", regexp.MustCompile(`\bT-0\d{3}\b`)},
	{"test-file", regexp.MustCompile(`[\w./-]*_test\.go\b`)},
}

var (
	generatedHeader = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)
	directivePrefix = []string{"//go:", "//lint:", "//nolint", "//export", "//line "}
	skippedDirs     = map[string]bool{"testdata": true, "vendor": true, "node_modules": true, ".git": true}
)

// Refusal is one comment line that cites history.
type Refusal struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Pattern string `json:"pattern"`
	Text    string `json:"text"`
}

// Exception is a refused comment kept by a reviewed directive.
type Exception struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Block is a run of comment lines at or over longBlock.
type Block struct {
	File  string `json:"file"`
	Line  int    `json:"line"`
	Lines int    `json:"lines"`
}

// Package is the comment weight of one directory's production files.
type Package struct {
	Dir          string  `json:"dir"`
	CommentLines int     `json:"comment_lines"`
	CodeLines    int     `json:"code_lines"`
	Ratio        float64 `json:"ratio"`
}

// Report is everything one run found.
type Report struct {
	Refused    []Refusal   `json:"refused"`
	Exceptions []Exception `json:"exceptions"`
	LongBlocks []Block     `json:"long_blocks"`
	Packages   []Package   `json:"packages"`
}

// Scan reads every production Go file under root and reports its comments.
// A file that does not parse is an error: the tool cannot judge it.
func Scan(root string) (Report, error) {
	rep := Report{Refused: []Refusal{}, Exceptions: []Exception{}, LongBlocks: []Block{}, Packages: []Package{}}
	pkgs := map[string]*Package{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		return scanFile(path, filepath.ToSlash(rel), &rep, pkgs)
	})
	if err != nil {
		return rep, err
	}
	for _, p := range pkgs {
		if p.CodeLines > 0 {
			p.Ratio = float64(p.CommentLines) / float64(p.CodeLines)
		}
		rep.Packages = append(rep.Packages, *p)
	}
	sort.Slice(rep.Packages, func(i, j int) bool { return rep.Packages[i].Dir < rep.Packages[j].Dir })
	return rep, nil
}

func scanFile(path, rel string, rep *Report, pkgs map[string]*Package) error {
	src, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		return err
	}
	line := func(p token.Pos) int { return fset.Position(p).Line }
	pkgLine := line(f.Package)

	skipGroup := map[*ast.CommentGroup]bool{}
	for _, g := range f.Comments {
		if line(g.End()) >= pkgLine {
			break
		}
		for _, c := range g.List {
			if generatedHeader.MatchString(c.Text) {
				return nil
			}
		}
		if g == f.Comments[0] {
			head := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(g.List[0].Text, "//"), "/*"))
			if strings.HasPrefix(head, "Copyright") || strings.HasPrefix(head, "SPDX-License-Identifier") {
				skipGroup[g] = true
			}
		}
	}

	// Example functions are documentation that runs: their text is exempt.
	type span struct{ from, to int }
	var examples []span
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "Example") {
			continue
		}
		from := line(fn.Pos())
		if fn.Doc != nil {
			from = line(fn.Doc.Pos())
		}
		examples = append(examples, span{from, line(fn.End())})
	}
	inExample := func(n int) bool {
		for _, s := range examples {
			if n >= s.from && n <= s.to {
				return true
			}
		}
		return false
	}

	// One line per comment line, with its text, so a block comment is
	// matched line by line and reported at the line it sits on.
	type cline struct {
		n    int
		text string
	}
	var all []cline
	directives := map[int]string{}
	commentLine := map[int]bool{}
	for _, g := range f.Comments {
		for _, c := range g.List {
			start := line(c.Pos())
			for i, t := range strings.Split(c.Text, "\n") {
				n := start + i
				if strings.HasPrefix(c.Text, "/*") || strings.HasPrefix(strings.TrimSpace(t), "//") {
					commentLine[n] = true
				}
				if strings.HasPrefix(strings.TrimSpace(t), historyDirective) {
					directives[n] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), historyDirective))
				}
				if !skipGroup[g] {
					all = append(all, cline{n, t})
				}
			}
		}
	}
	// A line is comment-only when nothing but a comment is on it; a trailing
	// comment after code counts as code.
	lines := strings.Split(string(src), "\n")
	for n := range commentLine {
		if n-1 < len(lines) {
			t := strings.TrimSpace(lines[n-1])
			if !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "/*") && !blockInterior(fset, f, n) {
				delete(commentLine, n)
			}
		}
	}

	groupStart := map[int]int{}
	for _, g := range f.Comments {
		for n := line(g.Pos()); n <= line(g.End()); n++ {
			groupStart[n] = line(g.Pos())
		}
	}
	for _, c := range all {
		if inExample(c.n) || isDirective(c.text) {
			continue
		}
		for _, p := range patterns {
			if !p.re.MatchString(c.text) {
				continue
			}
			reason, kept := directives[c.n-1]
			if !kept {
				reason, kept = directives[groupStart[c.n]-1]
			}
			switch {
			case kept && reason != "":
				rep.Exceptions = append(rep.Exceptions, Exception{rel, c.n, reason})
			case kept:
				rep.Refused = append(rep.Refused, Refusal{rel, c.n, "exception-reason", strings.TrimSpace(c.text)})
			default:
				rep.Refused = append(rep.Refused, Refusal{rel, c.n, p.id, strings.TrimSpace(c.text)})
			}
			break
		}
	}

	for _, g := range f.Comments {
		if n := line(g.End()) - line(g.Pos()) + 1; n >= longBlock && !skipGroup[g] {
			rep.LongBlocks = append(rep.LongBlocks, Block{rel, line(g.Pos()), n})
		}
	}

	dir := filepath.ToSlash(filepath.Dir(rel))
	p := pkgs[dir]
	if p == nil {
		p = &Package{Dir: dir}
		pkgs[dir] = p
	}
	for i, l := range lines {
		switch {
		case commentLine[i+1]:
			p.CommentLines++
		case strings.TrimSpace(l) != "":
			p.CodeLines++
		}
	}
	return nil
}

// blockInterior reports whether line n lies inside a multi-line /* */ comment.
func blockInterior(fset *token.FileSet, f *ast.File, n int) bool {
	for _, g := range f.Comments {
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, "/*") && fset.Position(c.Pos()).Line <= n && n <= fset.Position(c.End()).Line {
				return true
			}
		}
	}
	return false
}

func isDirective(text string) bool {
	t := strings.TrimSpace(text)
	for _, p := range directivePrefix {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// Text renders the report for a person: what is refused, what is kept by
// exception, which blocks are long and one totals line.
func (r Report) Text() string {
	var b strings.Builder
	for _, x := range r.Refused {
		fmt.Fprintf(&b, "REFUSED %s:%d %s: %s\n", x.File, x.Line, x.Pattern, x.Text)
	}
	for _, x := range r.Exceptions {
		fmt.Fprintf(&b, "EXCEPTION %s:%d %s\n", x.File, x.Line, x.Reason)
	}
	for _, x := range r.LongBlocks {
		fmt.Fprintf(&b, "LONG %s:%d %d\n", x.File, x.Line, x.Lines)
	}
	var cm, code int
	for _, p := range r.Packages {
		cm += p.CommentLines
		code += p.CodeLines
	}
	ratio := 0.0
	if code > 0 {
		ratio = float64(cm) / float64(code)
	}
	fmt.Fprintf(&b, "comments: %d refused, %d exception(s), %d long block(s), %d comment line(s) over %d code line(s) (%.2f)\n",
		len(r.Refused), len(r.Exceptions), len(r.LongBlocks), cm, code, ratio)
	return b.String()
}
