from pathlib import Path

import yaml

from sovereign_evals.runner import resolve_suite
from sovereign_evals.schemas import load_schema, validation_errors
from sovereign_evals.smoke.checks import REGISTRY

SMOKE = Path(resolve_suite("smoke"))


def test_smoke_suite_validates():
    suite = yaml.safe_load(SMOKE.read_text())
    assert validation_errors(suite, "eval-suite") == []


def test_smoke_check_types_are_registered():
    suite = yaml.safe_load(SMOKE.read_text())
    unknown = {c["type"] for c in suite["checks"]} - set(REGISTRY)
    assert not unknown


def test_registry_matches_schema_enum():
    enum = set(load_schema("eval-suite")["properties"]["checks"]["items"]["properties"]["type"]["enum"])
    assert set(REGISTRY) == enum
