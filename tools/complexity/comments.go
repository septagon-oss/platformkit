package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strings"
)

var historyPattern = regexp.MustCompile(`(?i)(review ?\d|finding ?\d|round ?\d|T-\d{3,}|\bD-?\d{2,}\b|decision \d|ADR-?\d|r\d+ finding|codex|grok|deepseek)|\b[0-9]{4}-[0-9]{2}-[0-9]{2}\b`)

func (m *measurement) comments(fset *token.FileSet, path string, groups []*ast.CommentGroup) {
	for _, group := range groups {
		start := fset.PositionFor(group.Pos(), false).Line
		span := fset.PositionFor(group.End(), false).Line - start + 1
		if span >= 15 {
			m.add("long_comment_blocks", path, "comment", start, span)
		}
		for _, comment := range group.List {
			line := fset.PositionFor(comment.Pos(), false).Line
			for text := range strings.SplitSeq(comment.Text, "\n") {
				if historyPattern.MatchString(text) {
					m.add("history_comment_lines", path, "comment", line, 1)
				}
				line++
			}
		}
	}
}
