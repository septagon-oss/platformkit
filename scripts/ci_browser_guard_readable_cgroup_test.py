"""The CI browser guard accepts readable container memory accounting."""

import pathlib
import shutil
import subprocess
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class BrowserGuardReadableCgroup(unittest.TestCase):
    def test_ci_guard_accepts_readable_cgroup_memory(self):
        layouts = {
            "v2": {
                "memory.max": "1073741824\n",
                "memory.current": "67108864\n",
                "memory.events": "low 0\nhigh 0\nmax 0\noom 0\noom_kill 0\n",
            },
            "v1": {
                "memory/memory.limit_in_bytes": "1073741824\n",
                "memory/memory.usage_in_bytes": "67108864\n",
                "memory/memory.failcnt": "0\n",
            },
        }
        for layout, files in layouts.items():
            with self.subTest(layout=layout), tempfile.TemporaryDirectory() as directory:
                fixture = pathlib.Path(directory)
                (fixture / "scripts").mkdir()
                (fixture / ".gitea/workflows").mkdir(parents=True)
                for name in ("Makefile", "go.mod", ".gitea/workflows/ci.yml"):
                    shutil.copyfile(ROOT / name, fixture / name)
                for name, content in files.items():
                    path = fixture / "cgroup" / name
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text(content, encoding="utf-8")
                for name in ("ci_browser_host_share.sh", "ci_browser_host_share_test.sh"):
                    content = (ROOT / "scripts" / name).read_text(encoding="utf-8")
                    if name == "ci_browser_host_share.sh":
                        # Redirect the platform paths; keep the reporter and its assertions intact.
                        self.assertIn("/sys/fs/cgroup/", content)
                        content = content.replace(
                            "/sys/fs/cgroup/", str(fixture / "cgroup") + "/"
                        )
                    (fixture / "scripts" / name).write_text(content, encoding="utf-8")
                result = subprocess.run(
                    ["bash", str(fixture / "scripts/ci_browser_host_share_test.sh")],
                    cwd=fixture,
                    stdin=subprocess.DEVNULL,
                    capture_output=True,
                    text=True,
                    timeout=30,
                )
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == "__main__":
    unittest.main()
