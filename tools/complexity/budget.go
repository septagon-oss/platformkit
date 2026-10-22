package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
)

const budgetFile = "complexity-budget.json"

var measures = [...]string{
	"cc_over_30", "files_over_800", "funcs_over_100", "history_comment_lines",
	"httpx_exports", "long_comment_blocks", "stutters",
}

type budget map[string]int64

func zeroBudget() budget {
	b := budget{}
	for _, key := range measures {
		b[key] = 0
	}
	return b
}

func decodeBudget(raw []byte) (budget, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return nil, fmt.Errorf("%s: expected one object", budgetFile)
	}
	b, known := budget{}, zeroBudget()
	for d.More() {
		token, err := d.Token()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", budgetFile, err)
		}
		key, ok := token.(string)
		_, valid := known[key]
		_, duplicate := b[key]
		if !ok || !valid || duplicate {
			return nil, fmt.Errorf("%s: unknown or duplicate key %q", budgetFile, key)
		}
		// Decode through any so JSON strings cannot be accepted as numbers.
		var input any
		if err := d.Decode(&input); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", budgetFile, key, err)
		}
		value, ok := input.(json.Number)
		n, err := strconv.ParseInt(string(value), 10, 64)
		if !ok || err != nil || n < 0 || len(value) == 0 || value[0] == '-' {
			return nil, fmt.Errorf("%s: %s: expected nonnegative integer, got %v", budgetFile, key, input)
		}
		b[key] = n
	}
	if _, err := d.Token(); err != nil {
		return nil, fmt.Errorf("%s: %w", budgetFile, err)
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: trailing input", budgetFile)
	}
	for _, key := range measures {
		if _, ok := b[key]; !ok {
			return nil, fmt.Errorf("%s: missing key %s", budgetFile, key)
		}
	}
	return b, nil
}

func countProblems(counts, ceilings budget) []string {
	var problems []string
	for _, key := range measures {
		n, ceiling := counts[key], ceilings[key]
		switch {
		case n > ceiling:
			problems = append(problems, fmt.Sprintf("OVER %s: count %d > ceiling %d (+%d)", key, n, ceiling, n-ceiling))
		case n < ceiling:
			problems = append(problems, fmt.Sprintf("LOWER %s: count %d < ceiling %d; set ceiling to %d", key, n, ceiling, n))
		}
	}
	return problems
}
