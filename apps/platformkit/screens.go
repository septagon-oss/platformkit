package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/pkit"
)

func screenCommand(args []string) error {
	flags := flag.NewFlagSet("screens", flag.ContinueOnError)
	configPath := flags.String("config", "config.yaml", "Composition configuration")
	out := flags.String("out", "ui/screens/testdata/screens.json", "Reviewed screen artifact")
	base := flags.String("base", "origin/main", "Compatibility merge-base reference")
	update := flags.Bool("update", false, "Explicitly update the reviewed artifact")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	inventory, err := referenceScreens(cfg)
	if err != nil {
		return err
	}
	if err := screenBaselines(inventory, *out, *base); err != nil {
		return err
	}
	data, err := inventory.Bytes()
	if err != nil {
		return err
	}
	if !*update {
		existing, err := os.ReadFile(*out)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, existing) {
			return fmt.Errorf("%s: screen artifact is stale; review and run screens -update", *out)
		}
		return nil
	}
	tmp, err := os.CreateTemp("", "screens-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), *out)
}

func referenceScreens(cfg config.Config) (screen.Inventory, error) {
	var first screen.Inventory
	var encoded []byte
	for _, environment := range []pkit.Environment{pkit.Development, pkit.Production} {
		ref := composeReference(cfg, environment)
		inventory, err := ref.app.Screens(context.Background(), ref.once)
		if err != nil {
			return screen.Inventory{}, err
		}
		data, err := inventory.Bytes()
		if err != nil {
			return screen.Inventory{}, err
		}
		if encoded != nil && !bytes.Equal(encoded, data) {
			return screen.Inventory{}, fmt.Errorf("screens: development and production contracts differ")
		}
		first, encoded = inventory, data
	}
	return first, nil
}

func screenBaselines(candidate screen.Inventory, path, base string) error {
	if local, err := os.ReadFile(path); err == nil {
		if err := compatibleScreens(candidate, local); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	merged, err := exec.Command("git", "merge-base", "HEAD", base).Output()
	if err != nil {
		return fmt.Errorf("screens: unavailable base %s: %w", base, err)
	}
	commit := strings.TrimSpace(string(merged))
	listed, err := exec.Command("git", "ls-tree", "--name-only", commit, "--", filepath.ToSlash(path)).Output()
	if err != nil {
		return fmt.Errorf("screens: read baseline tree: %w", err)
	}
	if len(bytes.TrimSpace(listed)) == 0 {
		return nil
	}
	data, err := exec.Command("git", "show", commit+":"+filepath.ToSlash(path)).Output()
	if err != nil {
		return fmt.Errorf("screens: read baseline artifact: %w", err)
	}
	return compatibleScreens(candidate, data)
}

func compatibleScreens(candidate screen.Inventory, data []byte) error {
	previous, err := screen.ParseSnapshot(data)
	if err != nil {
		return err
	}
	return candidate.Compatible(previous)
}
