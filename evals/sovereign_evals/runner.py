"""Suite runner: load a suite definition, execute checks, emit reports."""

from __future__ import annotations

import datetime
import html
import json
import os
import time
from pathlib import Path

import yaml

import sovereign_evals.benchmark  # noqa: F401 — registers benchmark check types
from sovereign_evals.endpoints import Endpoints
from sovereign_evals.schemas import validation_errors
from sovereign_evals.smoke.checks import REGISTRY, SKIPPED, SuiteContext


def suites_dir() -> Path:
    if env := os.environ.get("SOVEREIGN_SUITES_DIR"):
        return Path(env)
    here = Path(__file__).resolve()
    for parent in here.parents:
        candidate = parent / "suites"
        if candidate.is_dir():
            return candidate
    raise FileNotFoundError("suites/ directory not found; set SOVEREIGN_SUITES_DIR")


def resolve_suite(name_or_path: str) -> Path:
    path = Path(name_or_path)
    if path.is_file():
        return path
    candidate = suites_dir() / f"{name_or_path}.yaml"
    if candidate.is_file():
        return candidate
    raise FileNotFoundError(f"suite not found: {name_or_path}")


def _html_report(suite: dict, results: list[dict], generated: str) -> str:
    rows = []
    for r in results:
        status = "SKIP" if r["skipped"] else ("PASS" if r["passed"] else "FAIL")
        color = {"PASS": "#2e7d32", "FAIL": "#c62828", "SKIP": "#9e9e9e"}[status]
        rows.append(
            f"<tr><td style='color:{color};font-weight:600'>{status}</td>"
            f"<td>{html.escape(r['id'])}</td><td>{html.escape(r['type'])}</td>"
            f"<td>{r['duration_ms']} ms</td><td>{html.escape(r['details'])}</td></tr>"
        )
    failed = sum(1 for r in results if not r["passed"] and not r["skipped"])
    verdict = "PASS" if failed == 0 else f"FAIL ({failed})"
    return f"""<!doctype html><meta charset="utf-8">
<title>Sovereign Evals — {html.escape(suite["name"])}</title>
<body style="font-family:system-ui;max-width:60rem;margin:2rem auto;padding:0 1rem">
<h1>Sovereign Evals — {html.escape(suite["name"])}: {verdict}</h1>
<p>{html.escape(suite["description"])}<br>Generated {generated}</p>
<table border="1" cellpadding="6" style="border-collapse:collapse;width:100%">
<tr><th>Status</th><th>Check</th><th>Type</th><th>Duration</th><th>Details</th></tr>
{''.join(rows)}
</table></body>"""


def run_suite(
    name_or_path: str,
    endpoints: Endpoints | None = None,
    report_dir: str | None = None,
) -> int:
    suite_path = resolve_suite(name_or_path)
    suite = yaml.safe_load(suite_path.read_text())
    schema_problems = validation_errors(suite, "eval-suite")
    if schema_problems:
        print(f"{suite_path}: invalid suite definition")
        for problem in schema_problems:
            print(f"  - {problem}")
        return 2

    ctx = SuiteContext(endpoints or Endpoints())
    results: list[dict] = []
    seen_ids: set[str] = set()

    for index, check in enumerate(suite["checks"]):
        check_type = check["type"]
        check_id = check.get("id") or check_type
        if check_id in seen_ids:
            check_id = f"{check_id}-{index}"
        seen_ids.add(check_id)
        started = time.monotonic()
        metrics = None
        try:
            outcome = REGISTRY[check_type](ctx, check.get("params") or {})
            if len(outcome) == 3:
                passed, details, metrics = outcome
            else:
                passed, details = outcome
        except Exception as exc:
            passed, details = False, f"{type(exc).__name__}: {exc}"
        skipped = details.startswith(SKIPPED)
        if skipped:
            details = details[len(SKIPPED) :]
        record = {
            "id": check_id,
            "type": check_type,
            "passed": passed,
            "skipped": skipped,
            "details": details,
            "duration_ms": int((time.monotonic() - started) * 1000),
        }
        if metrics is not None:
            record["metrics"] = metrics
        results.append(record)

    width = max(len(r["id"]) for r in results)
    for r in results:
        mark = "SKIP" if r["skipped"] else ("PASS" if r["passed"] else "FAIL")
        print(f"  {mark:4}  {r['id']:<{width}}  {r['details']}")
    failed = [r for r in results if not r["passed"] and not r["skipped"]]
    skipped = [r for r in results if r["skipped"]]
    print(
        f"\n{suite['name']}: {len(results) - len(failed) - len(skipped)} passed, "
        f"{len(failed)} failed, {len(skipped)} skipped"
    )

    if report_dir:
        generated = datetime.datetime.now().strftime("%Y-%m-%d %H:%M:%S")
        stamp = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
        out = Path(report_dir)
        out.mkdir(parents=True, exist_ok=True)
        report = {
            "suite": suite["name"],
            "generated": generated,
            "results": results,
            "failed": len(failed),
        }
        (out / f"{suite['name']}-{stamp}.json").write_text(json.dumps(report, indent=2))
        (out / f"{suite['name']}-{stamp}.html").write_text(_html_report(suite, results, generated))
        print(f"reports written to {out}/{suite['name']}-{stamp}.{{json,html}}")

    return 1 if failed else 0
