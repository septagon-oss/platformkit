#!/usr/bin/env bash
# The delivery report answers each question the brief asks under the pillar contract.
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "usage: $0 BRIEF IMPLEMENT" >&2
	exit 2
fi

python3 - "$1" "$2" <<'PY'
import pathlib
import re
import sys

brief = pathlib.Path(sys.argv[1]).read_text()
report = pathlib.Path(sys.argv[2]).read_text()
brief_section = brief.split("## The pillar contract", 1)[1].split("\n## ", 1)[0]
questions = re.findall(r"(?m)^\d+\. \*\*(.+?)\.\*\*", brief_section)
if len(questions) != 8:
    sys.exit(f"brief has {len(questions)} pillar questions; expected eight")
if "## 6. Pillar contract" not in report:
    sys.exit("delivery report has no pillar contract section")
report_section = report.split("## 6. Pillar contract", 1)[1].split("\n## 7.", 1)[0]
sections = re.split(r"(?m)^### (?:\d+\. )?", report_section)
answers = {}
for section in sections[1:]:
    title, _, body = section.partition("\n")
    answers[title.strip().rstrip(".").lower()] = body

failed = False
for question in questions:
    body = answers.get(question.lower())
    if body is None:
        print(f"MISSING pillar answer: {question}", file=sys.stderr)
        failed = True
        continue
    if "Not applicable:" in body:
        if not re.search(r"Not applicable:\s*\S", body):
            print(f"EMPTY reason: {question}", file=sys.stderr)
            failed = True
    elif not all(re.search(rf"(?m)^\s*(?:- )?{field}:\s*\S", body) for field in ("Path", "Command", "Output")):
        print(f"INCOMPLETE evidence: {question} needs Path, Command and Output", file=sys.stderr)
        failed = True

if failed:
    sys.exit(1)
print("pillar contract: all eight answers carry evidence or a reason")
PY
