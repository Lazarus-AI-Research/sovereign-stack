"""Sovereign Evals CLI.

Subcommands:
  conformance      Run the runtime contract conformance suite against a base URL.
  validate-config  Validate a YAML/JSON file against a contract schema.
  suite            Run an evaluation suite (design.md §19) — implemented in M5.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import yaml


def _cmd_conformance(args: argparse.Namespace) -> int:
    from sovereign_evals.conformance import run_conformance

    return run_conformance(
        base_url=args.base_url,
        api_key=args.api_key,
        wait_ready=args.wait_ready,
        report_path=args.report,
    )


def _cmd_validate_config(args: argparse.Namespace) -> int:
    from sovereign_evals.schemas import validation_errors

    path = Path(args.file)
    if path.suffix in (".yaml", ".yml"):
        instance = yaml.safe_load(path.read_text())
    else:
        instance = json.loads(path.read_text())
    errors = validation_errors(instance, args.schema)
    if errors:
        print(f"{path}: INVALID against {args.schema}")
        for error in errors:
            print(f"  - {error}")
        return 1
    print(f"{path}: valid against {args.schema}")
    return 0


def _cmd_suite(args: argparse.Namespace) -> int:
    from sovereign_evals.endpoints import Endpoints
    from sovereign_evals.runner import run_suite

    endpoints = Endpoints()
    if args.runtime_url:
        endpoints.runtime_base_url = args.runtime_url
    if args.gateway_url:
        endpoints.gateway_base_url = args.gateway_url
    if args.prometheus_url:
        endpoints.prometheus_base_url = args.prometheus_url
    if args.pgvector_dsn:
        endpoints.pgvector_dsn = args.pgvector_dsn
    if args.runtime_api_key:
        endpoints.runtime_api_key = args.runtime_api_key
    if args.gateway_api_key:
        endpoints.gateway_api_key = args.gateway_api_key
    return run_suite(args.suite, endpoints=endpoints, report_dir=args.report_dir)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="sovereign-evals")
    sub = parser.add_subparsers(dest="command", required=True)

    p_conf = sub.add_parser("conformance", help="run the runtime contract conformance suite")
    p_conf.add_argument("--base-url", default="http://localhost:8000")
    p_conf.add_argument("--api-key", default=None)
    p_conf.add_argument("--wait-ready", type=float, default=120.0, metavar="SECONDS")
    p_conf.add_argument("--report", default=None, metavar="PATH", help="write JSON report")
    p_conf.set_defaults(fn=_cmd_conformance)

    p_val = sub.add_parser("validate-config", help="validate a file against a contract schema")
    p_val.add_argument("--schema", required=True, help="schema name, e.g. runtime-config")
    p_val.add_argument("--file", required=True)
    p_val.set_defaults(fn=_cmd_validate_config)

    p_suite = sub.add_parser("suite", help="run an evaluation suite")
    p_suite.add_argument("suite", help="suite name or path")
    p_suite.add_argument("--report-dir", default="reports")
    p_suite.add_argument("--runtime-url", default=None)
    p_suite.add_argument("--gateway-url", default=None)
    p_suite.add_argument("--prometheus-url", default=None)
    p_suite.add_argument("--pgvector-dsn", default=None)
    p_suite.add_argument("--runtime-api-key", default=None)
    p_suite.add_argument("--gateway-api-key", default=None)
    p_suite.set_defaults(fn=_cmd_suite)

    args = parser.parse_args(argv)
    return args.fn(args)


if __name__ == "__main__":
    sys.exit(main())
