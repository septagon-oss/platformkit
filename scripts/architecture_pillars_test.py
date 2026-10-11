"""Check the pillar section and its full and shorthand source citations.

Run from the repository root: python3 -B scripts/architecture_pillars_test.py
"""

from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]
TOKEN = re.compile(
    r"([A-Za-z0-9_./-]+\.(?:go|sql|md|json|ts|sh|yaml|yml|rego|js)|Makefile|NOTICE)"
    r"(?::(\d+)(?:-(\d+))?)?|`:(\d+)(?:-(\d+))?`"
)


def citation_errors(section, read):
    last = None
    errors = []
    checked = 0
    for match in TOKEN.finditer(section):
        path, start, end, short_start, short_end = match.groups()
        if path:
            last = path
        start, end = (start, end) if path else (short_start, short_end)
        if start is None:
            continue
        checked += 1
        start, end = int(start), int(end or start)
        try:
            lines = read(last).splitlines()
        except (OSError, KeyError, TypeError):
            errors.append(f"{match[0]}: source {last!r} does not exist")
            continue
        if not 1 <= start <= end <= len(lines):
            errors.append(f"{last}:{start}-{end}: outside 1-{len(lines)}")
    return checked, errors


class ArchitecturePillars(unittest.TestCase):
    def setUp(self):
        self.document = (ROOT / "ARCHITECTURE.md").read_text()
        _, marker, rest = self.document.partition("\n## Every pillar, end to end\n")
        self.assertTrue(marker, "missing pillar section")
        self.section = rest.split("\n## ", 1)[0]

    def test_each_pillar_answers_the_five_questions_in_order(self):
        expected = [
            "Design tokens and components", "CSS and JavaScript",
            "Authorization: OPA", "Authentication: sessions, TOTP, and WebAuthn",
            "Locales: kit/locale", "Change control: modules/change",
            "Events: CloudEvents, the outbox, JetStream", "Notifications",
            "Object storage: modules/file over S3", "Cache: kit/cache over Valkey",
            "Metrics and traces: OpenTelemetry", "Audit", "End-to-end and flows",
            "Mobile",
        ]
        sections = re.split(r"^### (.+)\n", self.section, flags=re.MULTILINE)
        self.assertEqual(expected, sections[1::2])
        headings = re.findall(r"^## (.+)$", self.document, re.MULTILINE)
        position = headings.index("Every pillar, end to end")
        self.assertGreater(position, 0)
        self.assertEqual("Follow a request", headings[position - 1])
        for title, body in zip(sections[1::2], sections[2::2]):
            with self.subTest(pillar=title):
                self.assertEqual([
                    "Where it lives.", "Builds on.", "How the tenant crosses it.",
                    "How it is traced and audited.", "How an app extends it.",
                ], re.findall(r"^\*\*(.+?)\*\*", body, re.MULTILINE))

    def test_full_and_shorthand_citation_ranges_resolve(self):
        checked, errors = citation_errors(self.section, lambda path: (ROOT / path).read_text())
        self.assertGreater(checked, 0, "no source citations checked")
        self.assertEqual([], errors)

    def test_citation_reader_refuses_missing_reversed_and_outside_ranges(self):
        sources = {"kit/example.go": "first\nsecond\nthird\n"}
        for citation in ["kit/example.go:0", "kit/example.go:2-4",
                         "kit/example.go:3-2", "kit/missing.go:1",
                         "kit/example.go `:4`", "`:1`"]:
            with self.subTest(citation=citation):
                checked, errors = citation_errors(citation, sources.__getitem__)
                self.assertEqual(1, checked)
                self.assertTrue(errors)
        self.assertEqual((2, []), citation_errors(
            "kit/example.go:1-2 then `:3`", sources.__getitem__))


if __name__ == "__main__":
    unittest.main(verbosity=2)
