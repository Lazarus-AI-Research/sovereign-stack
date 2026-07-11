"""M1 exit criteria: contract schemas accept the spec's examples and the
shipped deploy config, and reject broken variants."""

import copy
import json
from pathlib import Path

import pytest
import yaml

from sovereign_evals.schemas import schemas_dir, validation_errors

REPO_ROOT = schemas_dir().parent
FIXTURES = Path(__file__).resolve().parents[1] / "sovereign_evals" / "conformance" / "fixtures"


@pytest.fixture()
def manifest() -> dict:
    return json.loads((FIXTURES / "runtime-manifest.json").read_text())


@pytest.fixture()
def runtime_config() -> dict:
    return yaml.safe_load((REPO_ROOT / "deploy" / "config" / "runtime.yaml").read_text())


def test_manifest_fixture_is_valid(manifest):
    assert validation_errors(manifest, "runtime-manifest") == []


def test_deploy_runtime_config_is_valid(runtime_config):
    assert validation_errors(runtime_config, "runtime-config") == []


def test_config_missing_roles_is_invalid(runtime_config):
    broken = copy.deepcopy(runtime_config)
    del broken["roles"]
    assert validation_errors(broken, "runtime-config")


def test_config_bad_memory_weight_is_invalid(runtime_config):
    broken = copy.deepcopy(runtime_config)
    broken["roles"]["generation"]["memory_weight"] = 250
    assert validation_errors(broken, "runtime-config")


def test_config_enabled_role_requires_model(runtime_config):
    broken = copy.deepcopy(runtime_config)
    del broken["roles"]["generation"]["model"]
    assert validation_errors(broken, "runtime-config")


def test_manifest_bad_state_is_invalid(manifest):
    broken = copy.deepcopy(manifest)
    broken["state"] = "exploded"
    assert validation_errors(broken, "runtime-manifest")


def test_manifest_wrong_topology_is_invalid(manifest):
    broken = copy.deepcopy(manifest)
    broken["topology"] = "two_processes"
    assert validation_errors(broken, "runtime-manifest")
