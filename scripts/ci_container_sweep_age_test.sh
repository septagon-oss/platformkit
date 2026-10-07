#!/usr/bin/env bash
# The sweep a job runs first removes the labelled containers older than its bound, and only those.
#
# scripts/ci_container_leak_test.sh reads the sweep step's text: that it lists with
# `docker ps -aq --filter label=pkit.ci.run`, that it says `docker rm -f`, and that its
# SWEEP_OLDER_THAN is at least twice the job's ceiling. It does not read the comparison between a
# container's age and that bound, so a sweep whose test is turned round, or deleted, still passes
# it — and that sweep removes the containers of every live job on the daemon that carries the
# label, another repository's included. This file runs each job's committed sweep body under
# `bash -e` against a stub `docker` that lists one container created four hours ago and one created
# five minutes ago, and asks that the old one is removed, the young one is not, and the step exits 0.
#
# Usage: ci_container_sweep_age_test.sh [workflows-dir]   (default: this repository's .gitea/workflows)
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
workflows="${1:-$root/.gitea/workflows}"
scratch="$(mktemp -d "${TMPDIR:-/tmp}/ci-sweep-age-XXXXXX")"
trap 'rm -rf "$scratch"' EXIT

mkdir -p "$scratch/bin"
cat >"$scratch/bin/docker" <<'STUB'
#!/usr/bin/env bash
# Answers the three calls a sweep makes; records every removal in $SWEEP_REMOVED.
case "$1" in
ps) printf 'old\nyoung\n' ;;
inspect)
	case "${*: -1}" in
	old) date -u -d '4 hours ago' +%Y-%m-%dT%H:%M:%S.123456789Z ;;
	young) date -u -d '5 minutes ago' +%Y-%m-%dT%H:%M:%S.123456789Z ;;
	*) exit 1 ;;
	esac
	;;
rm) echo "${*: -1}" >>"$SWEEP_REMOVED" ;;
*) exit 1 ;;
esac
STUB
chmod +x "$scratch/bin/docker"

python3 - "$workflows" "$scratch" <<'PY'
import glob, os, subprocess, sys, yaml

folder, scratch = sys.argv[1], sys.argv[2]
failures = swept = 0
for path in sorted(glob.glob(os.path.join(folder, "*.yml"))):
    name = os.path.basename(path)
    for job, body in ((yaml.safe_load(open(path)) or {}).get("jobs") or {}).items():
        steps = body.get("steps") or []
        if not steps or "label=pkit.ci.run" not in str(steps[0].get("run") or ""):
            continue
        swept += 1
        removed = os.path.join(scratch, f"{name}-{job}.removed")
        open(removed, "w").close()
        env = {k: str(v) for k, v in (steps[0].get("env") or {}).items()}
        env.update(PATH=f"{scratch}/bin:{os.environ['PATH']}", SWEEP_REMOVED=removed, HOME=scratch)
        result = subprocess.run(["bash", "-e", "-c", steps[0]["run"]], env=env,
                                stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=60)
        gone = open(removed).read().split()
        if result.returncode != 0:
            print(f"FAIL: {name} job {job}'s sweep exits {result.returncode}: {result.stderr.strip()}")
            failures += 1
        elif gone != ["old"]:
            print(f"FAIL: {name} job {job}'s sweep removed {gone or 'nothing'}; a four-hour-old labelled container is due and a five-minute-old one belongs to a live job")
            failures += 1
        else:
            print(f"ok   {name} job {job}: the sweep removes the four-hour-old container and leaves the five-minute-old one ({result.stdout.strip().splitlines()[-1]})")
if swept == 0:
    print(f"FAIL: no job under {folder} sweeps label=pkit.ci.run in its first step")
    failures += 1
sys.exit(1 if failures else 0)
PY
