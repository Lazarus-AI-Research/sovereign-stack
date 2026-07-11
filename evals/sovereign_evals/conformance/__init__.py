"""Runtime contract conformance harness (docs/runtime-contract.md).

Runs shape and behavior checks against any Sovereign Runtime base URL. Every
runtime image — real, mock, or host-agent-backed — must be green before
release.
"""

from sovereign_evals.conformance.runner import run_conformance

__all__ = ["run_conformance"]
