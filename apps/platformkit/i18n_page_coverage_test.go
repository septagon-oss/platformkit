package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestCoverageReportPrintsEachPagesNumeratorAndDenominator(t *testing.T) {
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	saved := os.Stdout
	os.Stdout = writer
	t.Cleanup(func() { os.Stdout = saved; _ = writer.Close() })
	measurement := report{Version: floorVersion, Pages: map[string]coverage{
		"GET /first":  {Wrapped: 1, Readable: 2},
		"GET /second": {Wrapped: 2, Readable: 4},
	}}
	check(t, measurement, measurement, nil, nil, nil)
	os.Stdout = saved
	_ = writer.Close()
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	for page, ratio := range map[string]string{"GET /first": "1/2", "GET /second": "2/4"} {
		found := false
		for line := range strings.SplitSeq(string(out), "\n") {
			found = found || strings.Contains(line, page) && strings.Contains(line, ratio)
		}
		if !found {
			t.Errorf("no per-page measurement for %s (%s) in:\n%s", page, ratio, out)
		}
	}
}
