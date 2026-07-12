"""§25 lifecycle and failure-behavior tests against the runtime contract.

Spawns the mock runtime image in injected-failure modes and asserts the
contract's honesty guarantees (design.md §3.2–3.4): liveness never depends
on model readiness, failure states stay alive and diagnosable, and
configuration correction recovers without a crash loop.

Every runtime image must pass these; the mock runs in CI, real images run
the same suite wherever they can execute (docker required).

Run: pytest tests/runtime-contract -v
Requires: docker daemon, the sovereign-runtime-mock:dev image
(docker build -t sovereign-runtime-mock:dev runtime/mock).
"""

from __future__ import annotations

import json
import subprocess
import time
import uuid
from pathlib import Path

import httpx
import pytest

IMAGE = "sovereign-runtime-mock:dev"
REPO_ROOT = Path(__file__).resolve().parents[2]
GOOD_CONFIG = REPO_ROOT / "deploy" / "config" / "runtime.yaml"


def docker_available() -> bool:
    try:
        return subprocess.run(
            ["docker", "info"], capture_output=True, timeout=15
        ).returncode == 0
    except Exception:
        return False


pytestmark = pytest.mark.skipif(not docker_available(), reason="docker not available")


class RuntimeContainer:
    def __init__(self, *, config_path: Path | None, env: dict[str, str], port: int):
        self.name = f"contract-test-{uuid.uuid4().hex[:8]}"
        self.port = port
        command = [
            "docker", "run", "-d", "--name", self.name,
            "-p", f"{port}:8000",
        ]
        if config_path is not None:
            command += ["-v", f"{config_path}:/runtime-config/runtime.yaml:ro"]
        command += ["-e", "SOVEREIGN_RUNTIME_CONFIG=/runtime-config/runtime.yaml"]
        for key, value in env.items():
            command += ["-e", f"{key}={value}"]
        command += [IMAGE]
        subprocess.run(command, check=True, capture_output=True)

    def base_url(self) -> str:
        return f"http://127.0.0.1:{self.port}"

    def get(self, path: str) -> httpx.Response:
        return httpx.get(self.base_url() + path, timeout=10)

    def wait_alive(self, timeout: float = 30) -> None:
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            try:
                if self.get("/health/live").status_code == 200:
                    return
            except httpx.HTTPError:
                pass
            time.sleep(0.5)
        raise AssertionError("runtime never became alive")

    def wait_state(self, state: str, timeout: float = 30) -> None:
        deadline = time.monotonic() + timeout
        last = None
        while time.monotonic() < deadline:
            try:
                last = self.get("/health/live").json().get("state")
                if last == state:
                    return
            except httpx.HTTPError:
                pass
            time.sleep(0.5)
        raise AssertionError(f"state never reached {state!r} (last: {last!r})")

    def restart_count(self) -> int:
        out = subprocess.run(
            ["docker", "inspect", self.name, "--format", "{{.RestartCount}}"],
            check=True, capture_output=True, text=True,
        )
        return int(out.stdout.strip())

    def running(self) -> bool:
        out = subprocess.run(
            ["docker", "inspect", self.name, "--format", "{{.State.Running}}"],
            capture_output=True, text=True,
        )
        return out.stdout.strip() == "true"

    def restart(self) -> None:
        subprocess.run(["docker", "restart", self.name], check=True, capture_output=True)

    def remove(self) -> None:
        subprocess.run(["docker", "rm", "-f", self.name], capture_output=True)


@pytest.fixture
def runtime(request):
    containers: list[RuntimeContainer] = []

    def spawn(**kwargs) -> RuntimeContainer:
        container = RuntimeContainer(**kwargs)
        containers.append(container)
        return container

    yield spawn
    for container in containers:
        container.remove()


def test_configuration_error_stays_alive_and_diagnosable(runtime):
    """§3.2: a configuration error must not terminate the container, and the
    control surface stays available with structured errors."""
    container = runtime(
        config_path=GOOD_CONFIG,
        env={"MOCK_FAIL_MODE": "configuration_error", "MOCK_STATE_DELAY": "0.1"},
        port=18975,
    )
    container.wait_alive()
    container.wait_state("configuration_error")

    ready = container.get("/health/ready")
    assert ready.status_code == 503
    assert ready.json()["ready"] is False

    errors = container.get("/runtime/errors").json()["errors"]
    assert errors and errors[0]["code"] == "CONFIG_INVALID"
    assert errors[0]["recoverable"] is True

    # the manifest still serves and is shape-complete
    manifest = container.get("/runtime/manifest").json()
    assert manifest["state"] == "configuration_error"
    assert "generation" in manifest["roles"]

    time.sleep(2)
    assert container.running(), "container exited on configuration error"
    assert container.restart_count() == 0, "crash loop detected"


def test_missing_config_is_configuration_error(runtime):
    """§25 lifecycle: invalid runtime config → configuration_error, alive."""
    container = runtime(config_path=None, env={"MOCK_STATE_DELAY": "0.1"}, port=18976)
    container.wait_alive()
    container.wait_state("configuration_error")
    errors = container.get("/runtime/errors").json()["errors"]
    assert any(e["code"] == "CONFIG_INVALID" for e in errors)
    assert container.running()


def test_degraded_role_is_honest(runtime):
    """§3.4: one role down → degraded, not ready, healthy role reported
    truthfully, container alive."""
    container = runtime(
        config_path=GOOD_CONFIG,
        env={"MOCK_FAIL_MODE": "degraded_embedding", "MOCK_STATE_DELAY": "0.1"},
        port=18977,
    )
    container.wait_alive()
    container.wait_state("degraded")

    health = container.get("/health").json()
    assert health["roles"]["generation"]["status"] == "healthy"
    assert health["roles"]["embedding"]["status"] == "unhealthy"
    assert container.get("/health/ready").status_code == 503

    codes = {e["code"] for e in container.get("/runtime/errors").json()["errors"]}
    assert "MODEL_LOAD_FAILED" in codes
    assert container.running()


def test_runtime_error_reported(runtime):
    """§25 failure behavior: process-fatal class errors are reported as
    runtime_error while the control API stays reachable."""
    container = runtime(
        config_path=GOOD_CONFIG,
        env={"MOCK_FAIL_MODE": "runtime_error", "MOCK_STATE_DELAY": "0.1"},
        port=18978,
    )
    container.wait_alive()
    container.wait_state("runtime_error")
    errors = container.get("/runtime/errors").json()["errors"]
    assert any(e["code"] == "ENGINE_DEAD" and e["recoverable"] is False for e in errors)


def test_recovery_after_config_fix_without_crash_loop(runtime, tmp_path):
    """§25 lifecycle: configuration correction without crash loop — the SAME
    container goes configuration_error → (operator fixes the config file,
    exactly what Sovereign Control does) → restart → healthy."""
    config = tmp_path / "runtime.yaml"
    config.write_text("roles: [broken")  # invalid YAML

    container = runtime(config_path=config, env={"MOCK_STATE_DELAY": "0.1"}, port=18979)
    container.wait_alive()
    container.wait_state("configuration_error")
    assert container.restart_count() == 0

    # Host-side fix propagates through the ro bind mount; a single explicit
    # restart applies it — never a crash loop.
    config.write_text(GOOD_CONFIG.read_text())
    container.restart()
    container.wait_alive()
    container.wait_state("healthy")

    ready = container.get("/health/ready")
    assert ready.status_code == 200 and ready.json()["ready"] is True
    assert container.get("/runtime/errors").json()["errors"] == []
    assert container.running()


def test_healthy_manifest_matches_contract(runtime):
    """The §14 manifest from a healthy runtime validates against the schema."""
    container = runtime(config_path=GOOD_CONFIG, env={"MOCK_STATE_DELAY": "0.1"}, port=18980)
    container.wait_alive()
    container.wait_state("healthy")

    import sys

    sys.path.insert(0, str(REPO_ROOT / "evals"))
    from sovereign_evals.schemas import validation_errors

    manifest = container.get("/runtime/manifest").json()
    assert validation_errors(manifest, "runtime-manifest") == []
    payload = json.dumps(manifest)
    assert "sk-" not in payload and "change-me" not in payload, "secrets leaked into manifest (§22)"
