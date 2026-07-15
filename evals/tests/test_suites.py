import base64
from pathlib import Path

import yaml

from sovereign_evals.runner import resolve_suite
from sovereign_evals.schemas import load_schema, validation_errors
from sovereign_evals.smoke.checks import REGISTRY, _png_data_uri, _silent_wav_b64

SMOKE = Path(resolve_suite("smoke"))


def test_smoke_suite_validates():
    suite = yaml.safe_load(SMOKE.read_text())
    assert validation_errors(suite, "eval-suite") == []


def test_smoke_check_types_are_registered():
    suite = yaml.safe_load(SMOKE.read_text())
    unknown = {c["type"] for c in suite["checks"]} - set(REGISTRY)
    assert not unknown


def test_every_shipped_suite_validates_and_has_registered_checks():
    for path in SMOKE.parent.glob("*.yaml"):
        suite = yaml.safe_load(path.read_text())
        assert validation_errors(suite, "eval-suite") == [], path
        assert suite["checks"], path
        unknown = {check["type"] for check in suite["checks"]} - set(REGISTRY)
        assert not unknown, (path, unknown)


def test_registry_matches_schema_enum():
    enum = set(load_schema("eval-suite")["properties"]["checks"]["items"]["properties"]["type"]["enum"])
    assert set(REGISTRY) == enum


def test_multimodal_smoke_fixtures_are_valid_media():
    png = base64.b64decode(_png_data_uri((1, 2, 3)).split(",", 1)[1])
    wav = base64.b64decode(_silent_wav_b64())
    assert png.startswith(b"\x89PNG\r\n\x1a\n")
    assert wav.startswith(b"RIFF") and wav[8:12] == b"WAVE"
