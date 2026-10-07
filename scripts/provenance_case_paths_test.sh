#!/usr/bin/env bash
# The architecture gate's provenance cases must resolve after test-file renames.
set -euo pipefail
cd "$(dirname "$0")/.."
python3 - <<'PY'
from pathlib import Path
import re
import shlex

source = Path('scripts/check_architecture_test.sh').read_text()
match = re.search(r'^for provenance_case in (.+); do$', source, re.MULTILINE)
assert match, 'architecture gate no longer enumerates its provenance cases'
paths = [Path('scripts') / name for name in shlex.split(match[1])]
assert paths, 'architecture gate runs no provenance cases'
for path in paths:
    assert path.is_file(), f'architecture gate invokes missing case: {path}'
    assert not re.search(r'review|probe|round[0-9]', path.name), path
print('provenance case paths: every invoked case exists and names its behavior')
PY
