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

// source is one parsed file with the line arithmetic its checks share.
type source struct {
	fset  *token.FileSet
	file  *ast.File
	lines []string
}

func (s *source) line(p token.Pos) int { return s.fset.Position(p).Line }

// cline is one line of one comment and the line it sits on.
type cline struct {
	n    int
	text string
}

func scanFile(path, rel string, rep *Report, pkgs map[string]*Package) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, text, parser.ParseComments)
	if err != nil {
		return err
	}
	s := &source{fset: fset, file: f, lines: strings.Split(string(text), "\n")}
	skip, generated := s.header()
	if generated {
		return nil
	}
	cls, directives, commentLine := s.commentLines(skip)
	s.refuse(rel, cls, directives, rep)
	for _, g := range f.Comments {
		if n := s.line(g.End()) - s.line(g.Pos()) + 1; n >= longBlock && !skip[g] {
			rep.LongBlocks = append(rep.LongBlocks, Block{rel, s.line(g.Pos()), n})
		}
	}
	dir := filepath.ToSlash(filepath.Dir(rel))
	p := pkgs[dir]
	if p == nil {
		p = &Package{Dir: dir}
		pkgs[dir] = p
	}
	for i, l := range s.lines {
		switch {
		case commentLine[i+1]:
			p.CommentLines++
		case strings.TrimSpace(l) != "":
			p.CodeLines++
		}
	}
	return nil
}

// header finds what precedes the package clause and is not the file's to
// judge: a generated-code marker exempts the whole file, a licence group only
// itself.
func (s *source) header() (skip map[*ast.CommentGroup]bool, generated bool) {
	skip = map[*ast.CommentGroup]bool{}
	pkgLine := s.line(s.file.Package)
	for i, g := range s.file.Comments {
		if s.line(g.End()) >= pkgLine {
			break
		}
		for _, c := range g.List {
			if generatedHeader.MatchString(c.Text) {
				return skip, true
			}
		}
		if i == 0 {
			head := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(g.List[0].Text, "//"), "/*"))
			if strings.HasPrefix(head, "Copyright") || strings.HasPrefix(head, "SPDX-License-Identifier") {
				skip[g] = true
			}
		}
	}
	return skip, false
}

// commentLines splits every comment into lines. directives maps a line to the
// reason written after a history directive; commentLine marks the lines that
// hold nothing but comment, which is what the ratio counts.
func (s *source) commentLines(skip map[*ast.CommentGroup]bool) (all []cline, directives map[int]string, commentLine map[int]bool) {
	directives = map[int]string{}
	commentLine = map[int]bool{}
	for _, g := range s.file.Comments {
		for _, c := range g.List {
			start := s.line(c.Pos())
			block := strings.HasPrefix(c.Text, "/*")
			for i, t := range strings.Split(c.Text, "\n") {
				n := start + i
				trimmed := strings.TrimSpace(t)
				if block || strings.HasPrefix(trimmed, "//") {
					commentLine[n] = true
				}
				if strings.HasPrefix(trimmed, historyDirective) {
					directives[n] = strings.TrimSpace(strings.TrimPrefix(trimmed, historyDirective))
				}
				if !skip[g] {
					all = append(all, cline{n, t})
				}
			}
		}
	}
	// A comment that trails code on its line leaves the line a code line.
	for n := range commentLine {
		if n-1 >= len(s.lines) {
			continue
		}
		t := strings.TrimSpace(s.lines[n-1])
		if !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "/*") && !s.blockInterior(n) {
			delete(commentLine, n)
		}
	}
	return all, directives, commentLine
}

// exampleSpans are the line ranges of Example functions, doc comment included:
// documentation that runs keeps its text.
func (s *source) exampleSpans() [][2]int {
	var spans [][2]int
	for _, d := range s.file.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || !strings.HasPrefix(fn.Name.Name, "Example") {
			continue
		}
		from := s.line(fn.Pos())
		if fn.Doc != nil {
			from = s.line(fn.Doc.Pos())
		}
		spans = append(spans, [2]int{from, s.line(fn.End())})
	}
	return spans
}

// refuse matches every comment line against the patterns and files it as a
// refusal, or as an exception when a directive with a reason sits above it or
// above its comment group.
func (s *source) refuse(rel string, all []cline, directives map[int]string, rep *Report) {
	spans := s.exampleSpans()
	groupStart := map[int]int{}
	for _, g := range s.file.Comments {
		for n := s.line(g.Pos()); n <= s.line(g.End()); n++ {
			groupStart[n] = s.line(g.Pos())
		}
	}
	for _, c := range all {
		if isDirective(c.text) || inSpan(spans, c.n) {
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
}

func inSpan(spans [][2]int, n int) bool {
	for _, s := range spans {
		if n >= s[0] && n <= s[1] {
			return true
		}
	}
	return false
}

// blockInterior reports whether line n lies inside a multi-line /* */ comment.
func (s *source) blockInterior(n int) bool {
	for _, g := range s.file.Comments {
		for _, c := range g.List {
			if strings.HasPrefix(c.Text, "/*") && s.line(c.Pos()) <= n && n <= s.line(c.End()) {
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
