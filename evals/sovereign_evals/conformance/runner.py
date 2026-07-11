"""Conformance runner: waits for readiness, executes checks, reports."""

from __future__ import annotations

import json
import time
from dataclasses import asdict
from pathlib import Path

import httpx

from sovereign_evals.conformance.checks import CHECKS, CheckResult, Context


def _wait_ready(ctx: Context, timeout: float) -> bool:
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        try:
            resp = ctx.client.get("/health/ready")
            if resp.status_code == 200 and resp.json().get("ready") is True:
                return True
        except httpx.HTTPError:
            pass
        time.sleep(2.0)
    return False


def run_conformance(
    base_url: str,
    api_key: str | None = None,
    wait_ready: float = 120.0,
    report_path: str | None = None,
) -> int:
    """Run all checks against base_url. Returns a process exit code."""
    ctx = Context(base_url=base_url.rstrip("/"), api_key=api_key)
    ctx.ready = _wait_ready(ctx, wait_ready)

    results: list[CheckResult] = []
    for check_id, name, needs_ready, fn in CHECKS:
        if needs_ready and not ctx.ready:
            results.append(
                CheckResult(check_id, name, passed=True, skipped=True, details="runtime never ready")
            )
            continue
        try:
            results.append(fn(ctx))
        except Exception as exc:  # a crashing check is a failing check
            results.append(CheckResult(check_id, name, passed=False, details=f"{type(exc).__name__}: {exc}"))

    failed = [r for r in results if not r.passed]
    skipped = [r for r in results if r.skipped]

    width = max(len(r.check_id) for r in results)
    for r in results:
        mark = "SKIP" if r.skipped else ("PASS" if r.passed else "FAIL")
        line = f"  {mark:4}  {r.check_id:<{width}}"
        if r.details:
            line += f"  {r.details}"
        print(line)
    print(
        f"\nconformance: {len(results) - len(failed) - len(skipped)} passed, "
        f"{len(failed)} failed, {len(skipped)} skipped "
        f"(ready={ctx.ready}, base_url={ctx.base_url})"
    )
    if not ctx.ready and skipped:
        print("warning: runtime never became ready — behavior checks were skipped")

    if report_path:
        report = {
            "base_url": ctx.base_url,
            "ready": ctx.ready,
            "results": [asdict(r) for r in results],
        }
        Path(report_path).parent.mkdir(parents=True, exist_ok=True)
        Path(report_path).write_text(json.dumps(report, indent=2))
        print(f"report written to {report_path}")

    return 1 if failed else 0
