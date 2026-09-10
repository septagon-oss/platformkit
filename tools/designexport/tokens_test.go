package main

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"testing"
)

func TestExportTokenOptInKeepsModesExplicitAndDoesNotReadInput(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		selection string
		modes     []string
	}{
		{"light", []string{"light"}}, {"dark", []string{"dark"}}, {"both", []string{"light", "dark"}},
	} {
		t.Run(tc.selection, func(t *testing.T) {
			input := new(failingReader)
			args := []string{"--tokens", tc.selection}
			snapshot, first := exportedSnapshot(t, args, input)
			_, again := exportedSnapshot(t, args, input)
			if snapshot.SourceTokens == nil || snapshot.CheckSourceContract("source-tokens.v1") != nil {
				t.Fatal("explicit token command did not emit its checked v2 contract")
			}
			var modes []string
			for _, mode := range snapshot.SourceTokens.Modes {
				modes = append(modes, mode.Mode)
			}
			if !slices.Equal(modes, tc.modes) || len(snapshot.Examples) != 0 || len(snapshot.Themes) != 2 || len(snapshot.Icons) == 0 || snapshot.CSS == "" {
				t.Fatal("foundation capture lost its normal context, changed selected modes or included component examples")
			}
			if input.called || !bytes.Equal(first, again) {
				t.Fatal("plain token export read stdin or was not deterministic")
			}
			if err := run(args, input, failingWriter{}); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("output error was lost: %v", err)
			}
		})
	}
}

func TestExportTokenOptInRejectsArgumentsBeforeEffects(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"--tokens"}, {"--tokens", ""}, {"--tokens", "LIGHT"}, {"--tokens", "light,dark"},
		{"--tokens", "both", "--props"}, {"--tokens", "light", "--proposal"}, {"--tokens", "dark", "--example", "anything"},
	} {
		input := new(failingReader)
		var output bytes.Buffer
		if err := run(args, input, &output); err == nil || input.called || output.Len() != 0 {
			t.Fatalf("invalid token arguments %q read stdin or wrote output: %v", args, err)
		}
	}
}
