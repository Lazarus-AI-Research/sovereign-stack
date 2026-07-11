"""sovereign-runtime-healthcheck for the mock image.

Contract (design.md §3.3): --live probes only /health/live and never depends
on model readiness. stdlib-only so it works in any state.
"""

import argparse
import json
import os
import sys
import urllib.request


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--live", action="store_true")
    parser.parse_args()

    port = os.environ.get("SOVEREIGN_RUNTIME_PORT", "8000")
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/health/live", timeout=5) as resp:
            body = json.loads(resp.read())
            return 0 if resp.status == 200 and body.get("status") == "alive" else 1
    except Exception:
        return 1


if __name__ == "__main__":
    sys.exit(main())
