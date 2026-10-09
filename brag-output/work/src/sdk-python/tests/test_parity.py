# SPDX-License-Identifier: Apache-2.0
"""The validation cases shared with the Go SDK give the same (field, code) pairs."""

import json
import os

import pytest

from compliance_engine_sdk import Registry

CASES = os.path.join(os.path.dirname(__file__), "..", "..", "sdk", "schema", "testdata", "validation-cases.json")

with open(CASES, encoding="utf-8") as f:
    DOC = json.load(f)


@pytest.mark.parametrize("case", DOC["cases"], ids=[c["name"] for c in DOC["cases"]])
def test_validation_case(case):
    schema = Registry.default().resolve(DOC["schema_version"])
    got = [{"field": e.field, "code": e.code} for e in schema.validate_record(case["entity"], case["record"])]
    assert got == case["errors"]
