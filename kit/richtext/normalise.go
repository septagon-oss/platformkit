package richtext

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
)

// codeRun is one indented code block: the source lines it occupies and the
// lines of code the parser read out of them, indentation already removed.
type codeRun struct {
	first, last int // zero-based source lines the block spans
	code        []string
}

// indentedRun reads a code block's own lines from the source ranges the parser
// recorded for it. It reports false when those ranges do not stand one per
// source line: the shape it does not recognise is left written as it was, which
// is the only alternative to guessing at what the data held.
func (d *Document) indentedRun(v *ast.CodeBlock, lineAt func(offset int) int) (codeRun, bool) {
	segments := v.Lines()
	if segments.Len() == 0 {
		return codeRun{}, false
	}
	last := segments.At(segments.Len() - 1)
	stop := last.Start
	if last.Stop > last.Start {
		stop = last.Stop - 1
	}
	run := codeRun{first: lineAt(segments.At(0).Start), last: lineAt(stop)}
	if run.last-run.first+1 != segments.Len() {
		return codeRun{}, false
	}
	for i := 0; i < segments.Len(); i++ {
		segment := segments.At(i)
		if segment.Start > len(d.source) || segment.Stop > len(d.source) {
			return codeRun{}, false
		}
		run.code = append(run.code, strings.TrimSuffix(string(d.source[segment.Start:segment.Stop]), "\n"))
	}
	return run, true
}

// fenceWidth is the fence that holds these lines and nothing closes it: one
// longer than the longest run of backticks the code contains, never under three,
// because a closing fence is only a fence at the opening one's width.
func fenceWidth(code []string) int {
	width := 3
	for _, line := range code {
		run := 0
		for i := 0; i < len(line); i++ {
			if line[i] != '`' {
				run = 0
				continue
			}
			run++
			if run+1 > width {
				width = run + 1
			}
		}
	}
	return width
}

// Normalise validates a value and returns LF-terminated canonical Markdown.
//
// The canonical form is checked against the document it came from before it is
// returned: it must parse, it must pass the validation the source passed, it must
// project to the same readable words, and rewriting it must change nothing. A
// value whose canonical form the parser would read differently is refused on the
// write that found it — the alternative is a committed row that the next write
// refuses and that renders as something other than what its author typed.
func Normalise(source string) (string, error) {
	d, err := Parse(source)
	if err != nil {
		return "", err
	}
	if issues := Validate(d); len(issues) > 0 {
		return "", &Refused{Issues: issues}
	}
	stored := canonical(d)
	// Cutting a line short can change what the next parse makes of it: `<p\t` is
	// a paragraph of text, the `<p` left after its tab is trimmed opens an HTML
	// block. So the stored value is parsed and validated in its own right, and
	// the issue names the line of the value that would have been stored.
	value, err := Parse(stored)
	if err != nil {
		return "", err
	}
	if issues := Validate(value); len(issues) > 0 {
		return "", &Refused{Issues: issues}
	}
	if canonical(value) != stored || PlainText(value) != PlainText(d) {
		return "", &Refused{Issues: []Issue{{
			Construct: "unstorable text", Line: 1,
			Remedy: "Reword the passage so that it reads the same once it is saved.",
			Class:  Correctable,
		}}}
	}
	return stored, nil
}

// canonical serialises a validated document as LF-terminated Markdown. It reads
// the source through the syntax tree, so every decision about what a line of the
// source means is the parser's, and Normalise verifies the result of that reading
// rather than trusting it.
func canonical(d *Document) string {
	lineAt := sourceLineIndex(d.source)
	hardBreak, setext := headingLines(d, lineAt)
	protected, bullets, runs := literalLines(d, lineAt)
	entries := storedLines(d, protected, runs)
	out := rewriteProse(entries, hardBreak, setext, bullets)
	return serialiseLines(out)
}

// SourceHash is SHA-256 of canonical UTF-8 Markdown, encoded in lowercase hex.
func SourceHash(source string) (string, error) {
	normal, err := Normalise(source)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(normal))
	return hex.EncodeToString(sum[:]), nil
}

type storedLine struct {
	text    string
	literal bool // Code lines retain their bytes and blank runs.
	source  int  // Original source line, or -1 for an inserted fence line.
}

func sourceLineIndex(source []byte) func(int) int {
	var newlines []int
	for i, b := range source {
		if b == '\n' {
			newlines = append(newlines, i)
		}
	}
	return func(offset int) int { return sort.SearchInts(newlines, offset) }
}

func headingLines(d *Document, lineAt func(int) int) (map[int]bool, map[int]int) {
	// A text node's hard-break flag is the parser's decision that trailing
	// spaces are syntax. Keep exactly two; trimming them would silently turn
	// an allowed hard break into a soft break on the next parse.
	hardBreak := map[int]bool{}
	setext := map[int]int{}
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if v, ok := n.(*ast.Text); ok && v.HardLineBreak() {
			line := lineAt(v.Segment.Stop)
			hardBreak[line] = true
		}
		if v, ok := n.(*ast.Heading); ok && n.Parent() == d.root && n.Lines().Len() == 1 {
			line := lineAt(n.Lines().At(0).Start)
			setext[line] = v.Level
		}
		return ast.WalkContinue, nil
	})
	return hardBreak, setext
}

func literalLines(d *Document, lineAt func(int) int) (map[int]bool, map[int]bool, []codeRun) {
	// Preserve literal code bytes through trimming, blank-line compaction
	// and heading rewriting.
	protected := map[int]bool{}
	bullets := map[int]bool{}
	var runs []codeRun
	protect := func(n ast.Node) {
		for i := 0; i < n.Lines().Len(); i++ {
			segment := n.Lines().At(i)
			last := segment.Start
			if segment.Stop > segment.Start {
				last = segment.Stop - 1
			}
			for line := lineAt(segment.Start); line <= lineAt(last); line++ {
				protected[line] = true
			}
		}
	}
	_ = ast.Walk(d.root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.List:
			// Normalize isolated unordered lists to one marker. Preserve ordered
			// starts and adjacent lists, whose differing markers keep them separate.
			if v.IsOrdered() || v.Marker == '-' {
				return ast.WalkContinue, nil
			}
			if _, beside := v.PreviousSibling().(*ast.List); beside {
				return ast.WalkContinue, nil
			}
			if _, beside := v.NextSibling().(*ast.List); beside {
				return ast.WalkContinue, nil
			}
			for item := v.FirstChild(); item != nil; item = item.NextSibling() {
				// The marker itself is not in the tree: the item's first block
				// starts just after it, on the marker's own line.
				if child := item.FirstChild(); child != nil && child.Lines().Len() > 0 {
					bullets[lineAt(child.Lines().At(0).Start)] = true
				}
			}
		case *ast.CodeBlock:
			protect(n)
			// Only a block of the document's own indented code moves into a
			// fence: inside a list item or a quotation the code is indented
			// relative to that container, and a fence at column zero would end
			// the container the code belongs to.
			if run, ok := d.indentedRun(v, lineAt); ok && v.Parent() == d.root {
				runs = append(runs, run)
			}
		case *ast.FencedCodeBlock:
			protect(n)
		}
		return ast.WalkContinue, nil
	})

	return protected, bullets, runs
}

func storedLines(d *Document, protected map[int]bool, runs []codeRun) []storedLine {
	lines := strings.Split(string(d.source), "\n")
	entries := make([]storedLine, 0, len(lines)+len(runs))
	for i, line := range lines {
		entries = append(entries, storedLine{text: line, literal: protected[i], source: i})
	}
	// Fence only parsed indented code blocks; indentation alone can also
	// mean a nested list or paragraph.
	if len(runs) > 0 {
		fenced := make([]storedLine, 0, len(entries))
		next := 0
		for _, run := range runs {
			if run.first < next || run.last >= len(entries) {
				continue
			}
			fenced = append(fenced, entries[next:run.first]...)
			marker := strings.Repeat("`", fenceWidth(run.code))
			fenced = append(fenced, storedLine{text: marker, source: -1})
			for _, line := range run.code {
				fenced = append(fenced, storedLine{text: line, literal: true, source: -1})
			}
			fenced = append(fenced, storedLine{text: marker, source: -1})
			next = run.last + 1
		}
		entries = append(fenced, entries[next:]...)
	}
	return entries
}

func rewriteProse(entries []storedLine, hardBreak map[int]bool, setext map[int]int, bullets map[int]bool) []storedLine {
	out := make([]storedLine, 0, len(entries))
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if entry.literal {
			out = append(out, entry)
			continue
		}
		line := strings.TrimRight(entry.text, " \t")
		if hardBreak[entry.source] && strings.HasSuffix(entry.text, "  ") {
			line += "  "
		}
		if bullets[entry.source] {
			lead := strings.TrimLeft(line, " \t")
			if len(lead) > 0 && strings.IndexByte("*+-", lead[0]) >= 0 {
				line = line[:len(line)-len(lead)] + "-" + lead[1:]
			}
		}
		// A Setext heading and its ATX spelling are the same parsed heading.
		// Store one spelling and drop the underline, leaving the block after it
		// as adjacent to the heading as it was written.
		if level := setext[entry.source]; level >= 2 && level <= 4 && i+1 < len(entries) &&
			!atxStart.MatchString(entry.text) && setextRule.MatchString(entries[i+1].text) {
			line = strings.Repeat("#", level) + " " + strings.TrimSpace(line)
			i++
		}
		entry.text = line
		out = append(out, entry)
	}
	return out
}

func serialiseLines(out []storedLine) string {
	for len(out) > 0 && out[0].text == "" && !out[0].literal {
		out = out[1:]
	}
	for n := len(out) - 1; n >= 0 && out[n].text == "" && !out[n].literal; n-- {
		out = out[:n]
	}
	if len(out) == 0 {
		return ""
	}
	// Empty runs are one block separator. Preserving line indentation retains
	// list and table structure, and a line of literal code keeps the blanks it
	// was written with, which is what makes a second pass byte-identical.
	compact := make([]storedLine, 0, len(out))
	for _, line := range out {
		if last := len(compact) - 1; line.text != "" || len(compact) == 0 ||
			compact[last].text != "" || compact[last].literal {
			compact = append(compact, line)
		}
	}
	text := make([]string, len(compact))
	for i, line := range compact {
		text[i] = line.text
	}
	return strings.Join(text, "\n") + "\n"
}
