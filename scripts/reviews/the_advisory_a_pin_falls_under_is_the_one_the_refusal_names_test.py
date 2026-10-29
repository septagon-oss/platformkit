#!/usr/bin/env python3
"""The advisory a pinned release falls under is the one the refusal names.

`scripts/go_mod_above_advisory_test.sh` answers for one package at a time and joins every advisory that
shares that answer into one sentence, so an entry can stop being reported while the file still exits 1.
What a reader of a refused CI step needs is that the sentence names the advisory that actually applies:
the ids of an entry the pin satisfies never appear, and the ids of the entry the pin fell under do. Both
halves are asked here of synthetic go.mod files only — never of the committed pins, which that script
owns — so a dependency raise elsewhere cannot refuse this case.

Run: python3 -m pytest -q \
    scripts/reviews/the_advisory_a_pin_falls_under_is_the_one_the_refusal_names_test.py
"""
import pathlib
import re
import subprocess
import tempfile
import unittest

GUARD = pathlib.Path(__file__).resolve().parents[1] / "go_mod_above_advisory_test.sh"
ENTRY = re.compile(
    r'"package":\s*"(?P<package>[^"]+)",\s*"source":\s*"(?P<source>[^"]+)",\s*'
    r'"fixed":\s*\((?P<fixed>[^)]*)\),\s*"ids":\s*\((?P<ids>[^)]*)\)'
)
NAMED = re.compile(r"GO-\d{4}-\d{4}")


def entries():
    """Every advisory the guard records: its package or `toolchain`, the release that closes it, its ids.

    Read from the guard's own table, so this case asks the pairing between a pin and an advisory rather
    than a copy of the list that could drift from it.
    """
    text = GUARD.read_text(encoding="utf-8")
    found = []
    for match in ENTRY.finditer(text):
        fixed = tuple(int(part) for part in match.group("fixed").replace("\n", " ").split(",") if part.strip())
        ids = tuple(part for part in re.findall(r"GO-\d{4}-\d{4}", match.group("ids")))
        found.append({"package": match.group("package"), "source": match.group("source"),
                      "fixed": fixed, "ids": ids})
    return found


def group_of(entry):
    return (entry["source"], entry["package"])


def release(fixed, source):
    """The release as a go.mod writes it: `go1.27.2` for the standard library, `v0.62.0` for a module."""
    return ("go" if source == "toolchain" else "v") + ".".join(str(part) for part in fixed)


def one_release_below(fixed):
    """The release the file below the fix: the last component that can fall, lowered by one."""
    below = list(fixed)
    for position in range(len(below) - 1, -1, -1):
        if below[position] > 0:
            below[position] -= 1
            return tuple(below)
    raise AssertionError(f"{fixed} has no release below it")


def written(pins):
    """A go.mod holding exactly the pins asked for: `pins` maps a group to the release it fixes at."""
    lines = ["module example.test/advisory", "", "go 1.26.6", ""]
    toolchain = [release(fixed, "toolchain")
                 for (source, _), fixed in pins.items() if source == "toolchain"]
    lines += [f"toolchain {toolchain[0]}", ""] if toolchain else []
    modules = [f"\t{package} {release(fixed, 'module')}"
               for (source, package), fixed in pins.items() if source == "module"]
    return "\n".join(lines + ["require (", *sorted(modules), ")", ""])


def run(directory):
    result = subprocess.run(["bash", str(GUARD), str(directory)], stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, timeout=60)
    output = result.stdout + result.stderr
    refused = [line for line in output.splitlines() if line.startswith("FAIL: go.mod")]
    named = {identifier for line in refused for identifier in NAMED.findall(line)}
    return result.returncode, refused, named


class AdvisoryNamesItsOwnRefusal(unittest.TestCase):
    def test_every_recorded_advisory_is_separable_from_its_neighbours(self):
        recorded = entries()
        self.assertTrue(recorded, f"{GUARD} records no advisory entry to separate")
        highest = {}
        for entry in recorded:
            group = group_of(entry)
            if group not in highest or entry["fixed"] > highest[group]:
                highest[group] = entry["fixed"]

        for scenario, pins in self.scenarios(recorded, highest):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as directory:
                path = pathlib.Path(directory, "go.mod")
                path.write_text(written(pins), encoding="utf-8")
                code, refused, named = run(path.parent)
                applies = [entry for entry in recorded if pins[group_of(entry)] < entry["fixed"]]
                satisfied_ids = {identifier for entry in recorded
                                 if pins[group_of(entry)] >= entry["fixed"] for identifier in entry["ids"]}
                self.assertEqual(code, 1 if applies else 0, refused)
                self.assertEqual(named & satisfied_ids, set(),
                                 f"{scenario}: a refusal accused an advisory the pins sit above: "
                                 f"{sorted(named & satisfied_ids)}")
                if applies:
                    expected_ids = {identifier for entry in applies for identifier in entry["ids"]}
                    self.assertTrue(named & expected_ids,
                                    f"{scenario}: the pins fall under {' '.join(sorted(expected_ids))} "
                                    f"and no refusal names them: {refused}")

    def test_a_pin_at_every_recorded_fix_is_accepted(self):
        # The accepted shape: every pin sitting at the highest fix the file records. A comparison that
        # refuses its own floor — an inverted `<`, a version this file cannot place — has no other way
        # of being caught, because the committed tree reaches this script only through this same branch.
        recorded = entries()
        highest = {}
        for entry in recorded:
            group = group_of(entry)
            if group not in highest or entry["fixed"] > highest[group]:
                highest[group] = entry["fixed"]
        with tempfile.TemporaryDirectory() as directory:
            path = pathlib.Path(directory, "go.mod")
            path.write_text(written(highest), encoding="utf-8")
            code, refused, _ = run(path.parent)
            self.assertEqual(code, 0, refused)

    @staticmethod
    def scenarios(recorded, highest):
        """One synthetic go.mod per recorded advisory: that advisory's group pinned one release below
        its fix, every other group pinned at the highest fix the file records."""
        for entry in recorded:
            pins = dict(highest)
            pins[group_of(entry)] = one_release_below(entry["fixed"])
            yield (f"{entry['package']} one release below {'.'.join(str(p) for p in entry['fixed'])} "
                   f"({', '.join(entry['ids'])})", pins)


if __name__ == "__main__":
    unittest.main()
