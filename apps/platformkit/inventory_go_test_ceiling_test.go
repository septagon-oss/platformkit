package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestTheInventoryQuotesItsDeclaredGoTestCeiling(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "loc-budget.json"))
	if err != nil {
		t.Fatal(err)
	}
	var budget struct {
		Buckets []struct {
			Name string
			Max  int
		}
	}
	if err := json.Unmarshal(data, &budget); err != nil {
		t.Fatal(err)
	}
	ceiling := 0
	for _, bucket := range budget.Buckets {
		if bucket.Name == "go_test" {
			ceiling = bucket.Max
		}
	}
	if ceiling <= 0 {
		t.Fatal("go_test must declare a positive ceiling")
	}
	data, err = os.ReadFile(filepath.Join(root, "tests", "INVENTORY.md"))
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile("(?m)^\\| `go_test` lines \\| ([0-9 ]+) against the ([0-9 ]+) ceiling \\|").FindAllSubmatch(data, -1)
	if len(rows) != 1 {
		t.Fatalf("want one numeric go_test count and ceiling row, got %d", len(rows))
	}
	count, err := strconv.Atoi(strings.ReplaceAll(string(rows[0][1]), " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	quoted, err := strconv.Atoi(strings.ReplaceAll(string(rows[0][2]), " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	if quoted != ceiling || count > ceiling {
		t.Errorf("inventory quotes %d against %d; declared go_test ceiling is %d", count, quoted, ceiling)
	}
}
