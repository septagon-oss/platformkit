"""A checkout's revision identifies its bytes even when Go also has a tag."""

import os
import subprocess


ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def test_the_checkout_revision_wins_over_the_module_tag():
    source = open(os.path.join(ROOT, "modules/admin/internal/mount.go"), encoding="utf-8").read()
    revision = source.index('if setting.Key == "vcs.revision"')
    module_version = source.index('if v := info.Main.Version', revision)
    assert revision < module_version, "the build stamp checks a module tag before the checkout revision"

    result = subprocess.run(
        ["go", "test", "./modules/admin/internal", "-run", "^TestTheFooterNamesTheStampTheRunningArtifactCarries$", "-count=1"],
        cwd=ROOT,
        capture_output=True,
        text=True,
        timeout=600,
    )
    assert result.returncode == 0, result.stdout + result.stderr
