# SPDX-License-Identifier: Apache-2.0
from datetime import datetime, timedelta, timezone

import pytest

from compliance_engine_sdk import (Batch, BatchBuilder, Manifest, ManifestError, Mode, Registry, UnsupportedVersionError,
                                   mark_derived, mark_not_applicable, meta_of, set_source_ref, validate_batch,
                                   validate_manifest_document)

REG = Registry.default()


def manifest(**kw):
    m = dict(name="vendor-inventory", version="1.0.0", schema_version="0.1.0",
             supplies={"ict_provider": ["provider_id_code", "legal_name", "hq_country"]}, modes=[Mode.FULL, Mode.INCREMENTAL])
    m.update(kw)
    return Manifest(**m)


def test_registry_resolves_patches():
    assert REG.versions == ["0.1.0"]
    assert REG.resolve("0.1.7").version == "0.1.0"
    with pytest.raises(UnsupportedVersionError):
        REG.resolve("0.2.0")
    with pytest.raises(UnsupportedVersionError):
        REG.resolve("one")
    e = REG.latest().entity("ict_provider")
    assert e.identity_key == ["provider_id_code"] and e.template == "B_05.01" and "legal_name" in e.field_names


def test_manifest_rules():
    manifest().validate(REG)
    with pytest.raises(ManifestError) as err:
        manifest(name=" ", supplies={"ict_provider": ["legal_name", "nope"], "spaceship": ["x"]}, modes=["sometimes"]).validate(REG)
    problems = "\n".join(err.value.problems)
    for want in ["name is required", "unknown field ict_provider.nope", "unknown entity 'spaceship'",
                 "not its identity field provider_id_code", "unknown mode 'sometimes'"]:
        assert want in problems
    with pytest.raises(ManifestError):
        manifest(schema_version="9.0.0").validate(REG)


def test_manifest_document():
    assert validate_manifest_document(manifest().to_dict()) == []
    errs = validate_manifest_document({"name": "x", "version": "1", "schema_version": "0.1.0", "supplies": {}, "modes": [], "extra": 1})
    pairs = [(e.field, e.code) for e in errs]
    assert ("extra", "unknown_field") in pairs
    assert ("modes", "minItems") in pairs


def test_builder_and_json_round_trip():
    at = datetime(2026, 10, 2, 9, 0, tzinfo=timezone(timedelta(hours=2)))
    b = (BatchBuilder(manifest(), "vendor-export").mode(Mode.FULL).batch_id("b-1").generated_at(at)
         .add("ict_provider", {"provider_id_code": "P1"}).add("ict_provider", {"provider_id_code": "P2"}).build())
    assert b.mode == Mode.FULL and b.batch_id == "b-1" and b.record_count() == 2
    d = b.to_dict()
    assert d["source"] == {"system": "vendor-export", "adapter": "vendor-inventory", "adapter_version": "1.0.0"}
    assert d["batch"] == {"batch_id": "b-1", "generated_at": "2026-10-02T07:00:00Z", "mode": "full"}
    assert Batch.from_json(b.to_json()).to_dict() == d
    with pytest.raises(ValueError):
        Batch.from_dict(dict(d, extra=1))
    with pytest.raises(ValueError):
        BatchBuilder(manifest(), "x").generated_at(datetime(2026, 1, 1))
    plain = Batch.from_dict({"schema_version": "0.1.0", "source": {"system": "s", "adapter": "a", "adapter_version": "1"}, "entities": {}})
    assert plain.mode == Mode.INCREMENTAL


def test_metadata_helpers():
    rec = {"provider_id_code": "P1"}
    set_source_ref(rec, "vendors/1")
    mark_not_applicable(rec, "parent_id_code", "parent_id_code", "parent_id_type")
    mark_derived(rec, "hq_country", "lookup", "eu-west-1")
    mark_derived(rec, "hq_country", "registry")
    assert meta_of(rec) == {"source_record_ref": "vendors/1", "not_applicable_fields": ["parent_id_code", "parent_id_type"],
                            "derived_fields": [{"field": "hq_country", "method": "registry"}]}
    assert REG.latest().validate_record("ict_provider", rec) == []
    plain = {"provider_id_code": "P2"}
    mark_not_applicable(plain)
    assert "_meta" not in plain


def test_validate_batch():
    b = BatchBuilder(manifest(), "s").add("ict_provider", {"provider_id_code": "P1"}).build()
    assert validate_batch(b, REG) == []
    rec = {"hq_country": "Ireland"}
    set_source_ref(rec, "row-7")
    b.entities["ict_provider"].append(rec)
    errs = validate_batch(b, REG)
    assert [(e.entity, e.index, e.field, e.code, e.source_record_ref) for e in errs] == [
        ("ict_provider", 1, "hq_country", "pattern", "row-7"), ("ict_provider", 1, "provider_id_code", "required", "row-7")]
    env = Batch.from_dict({"schema_version": "0.1", "source": {"system": "", "adapter": "a\x00", "adapter_version": "1"},
                           "batch": {"mode": "sometimes"}, "entities": {"spaceship": []}})
    codes = [(e.field, e.code) for e in env.validate_envelope(REG.latest())]
    for want in [("schema_version", "pattern"), ("source.system", "required"), ("source.adapter", "invalid_character"),
                 ("batch.mode", "enum"), ("entities.spaceship", "unknown_entity")]:
        assert want in codes
    future = Batch.from_dict({"schema_version": "7.0.0", "source": {"system": "s", "adapter": "a", "adapter_version": "1"}, "entities": {}})
    assert validate_batch(future, REG)[0].code == "unsupported_schema_version"
