"""Check the actual settings carriers against fresh and existing Postgres data."""
from contextlib import contextmanager
from pathlib import Path
import subprocess
import time
import unittest
import uuid

import yaml


ROOT = Path(__file__).resolve().parents[3]
EXPECTED = {"fsync": "off", "synchronous_commit": "off",
            "full_page_writes": "off", "max_connections": "256"}


class PostgresServiceLifecycle(unittest.TestCase):
    def docker(self, *args, check=True):
        result = subprocess.run(["docker", *args], stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, timeout=30)
        if check:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result

    @contextmanager
    def server(self, image, *, env=None, command=(), volume=None):
        args = ["run", "--detach", "--network", "none"]
        for key, value in (env or {"POSTGRES_PASSWORD": "platformkit"}).items():
            args.extend(["--env", f"{key}={value}"])
        if volume:
            args.extend(["--mount", f"type=volume,src={volume},dst=/var/lib/postgresql/data"])
        container = self.docker(*args, image, *command).stdout.strip()
        try:
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                logs = self.docker("logs", container).stdout
                initialized = ("PostgreSQL init process complete" in logs
                               or "Skipping initialization" in logs)
                if initialized:
                    result = self.docker("exec", container, "psql", "-U", "postgres",
                                         "-d", "postgres", "-Atc",
                                         "SELECT name || '=' || setting FROM pg_settings "
                                         "WHERE name IN ('fsync', 'synchronous_commit', "
                                         "'full_page_writes', 'max_connections') ORDER BY name",
                                         check=False)
                    if result.returncode == 0:
                        yield dict(line.split("=", 1) for line in result.stdout.splitlines())
                        return
                state = self.docker("inspect", "-f", "{{.State.Running}}", container).stdout.strip()
                self.assertEqual(state, "true", self.docker("logs", container).stdout)
                time.sleep(0.2)
            self.fail("Postgres did not become ready: " + self.docker("logs", container).stdout)
        finally:
            self.docker("rm", "--force", "--volumes", container)

    def test_each_ci_service_initializes_with_the_declared_settings(self):
        services = []
        for directory in (".gitea", ".github"):
            for path in sorted((ROOT / directory / "workflows").glob("*.y*ml")):
                workflow = yaml.safe_load(path.read_text())
                for job, definition in workflow.get("jobs", {}).items():
                    for name, service in definition.get("services", {}).items():
                        if service.get("image", "").startswith("postgres:"):
                            services.append((path, job, name, service))
        self.assertTrue(services, "No workflow Postgres services were exercised")
        for path, job, name, service in services:
            with self.subTest(workflow=str(path.relative_to(ROOT)), job=job, service=name):
                with self.server(service["image"], env=service["env"]) as settings:
                    self.assertEqual(settings, EXPECTED)

    def test_compose_overrides_an_existing_durable_cluster(self):
        service = yaml.safe_load((ROOT / "compose.yaml").read_text())["services"]["postgres"]
        volume = "postgres-settings-" + uuid.uuid4().hex
        self.docker("volume", "create", volume)
        try:
            with self.server(service["image"], volume=volume) as settings:
                self.assertEqual(settings["fsync"], "on")
                self.assertEqual(settings["full_page_writes"], "on")
                self.assertEqual(settings["synchronous_commit"], "on")
            with self.server(service["image"], command=service["command"], volume=volume) as settings:
                self.assertEqual(settings, EXPECTED)
        finally:
            self.docker("volume", "rm", volume)


if __name__ == "__main__":
    unittest.main()
