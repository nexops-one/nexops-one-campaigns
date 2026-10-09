# SPDX-License-Identifier: Apache-2.0
"""Canonical schemas and local (L1) validation.

The schemas are package data, copied from the engine repository's schema/
directory by ``go generate ./schema``; they are the same files the Go SDK
embeds, and the validation reports the same error codes as the Go SDK:
``required``, ``unknown_field``, otherwise the failing JSON Schema keyword
(``type``, ``pattern``, ``enum``, ``format``, ...). Fields are dotted paths.
"""

from __future__ import annotations

import json
import re
from dataclasses import asdict, dataclass, field
from datetime import date, datetime
from importlib import resources
from typing import Any, Dict, Iterable, List, Optional

from jsonschema import Draft202012Validator, FormatChecker

META_FIELD = "_meta"


class UnsupportedVersionError(ValueError):
    """A schema version no loaded schema can validate."""


@dataclass
class FieldError:
    """A field-level validation error, shaped like the engine's."""

    entity: str
    field: str
    code: str
    message: str
    index: int = -1
    source_record_ref: str = ""

    def to_dict(self) -> Dict[str, Any]:
        out = asdict(self)
        if not out["source_record_ref"]:
            del out["source_record_ref"]
        return out


@dataclass(frozen=True)
class Version:
    major: int
    minor: int
    patch: int

    @classmethod
    def parse(cls, s: str) -> "Version":
        m = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)", s or "")
        if not m:
            raise UnsupportedVersionError(f"version {s!r} is not MAJOR.MINOR.PATCH")
        return cls(int(m.group(1)), int(m.group(2)), int(m.group(3)))

    def __str__(self) -> str:
        return f"{self.major}.{self.minor}.{self.patch}"


# The checks the Go SDK asserts for the formats the canonical schema uses.
# jsonschema's own date-time check needs an extra package; this one keeps
# jsonschema the only dependency.
_RFC3339 = re.compile(
    r"^\d{4}-\d{2}-\d{2}[Tt]\d{2}:\d{2}:\d{2}(\.\d+)?([Zz]|[+-]\d{2}:\d{2})$"
)

_FORMATS = FormatChecker(formats=())


@_FORMATS.checks("date", raises=ValueError)
def _is_date(value: Any) -> bool:
    if not isinstance(value, str):
        return True
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
        return False
    date.fromisoformat(value)
    return True


@_FORMATS.checks("date-time", raises=ValueError)
def _is_date_time(value: Any) -> bool:
    if not isinstance(value, str):
        return True
    if not _RFC3339.match(value):
        return False
    normalized = re.sub(r"[Zz]$", "+00:00", value.replace("t", "T"))
    # Python before 3.11 parses at most 6 fractional digits.
    normalized = re.sub(r"(\.\d{6})\d+", r"\1", normalized)
    datetime.fromisoformat(normalized)
    return True


@dataclass
class Entity:
    """One canonical entity and its x-* annotations."""

    name: str
    title: str
    template: str
    identity_key: List[str]
    fields: Dict[str, Dict[str, Any]] = field(default_factory=dict)

    @property
    def field_names(self) -> List[str]:
        return sorted(self.fields)


def _path(parts: Iterable[Any]) -> str:
    return ".".join(str(p) for p in parts)


def _join(base: str, name: str) -> str:
    return f"{base}.{name}" if base else name


def errors_of(validator: Draft202012Validator, instance: Any, entity: str) -> List[FieldError]:
    """Validates instance and maps the errors to the engine's codes."""
    out: List[FieldError] = []
    for e in validator.iter_errors(instance):
        base = _path(e.absolute_path)
        if e.validator == "required":
            missing = [k for k in e.validator_value if isinstance(e.instance, dict) and k not in e.instance]
            for k in missing:
                out.append(FieldError(entity, _join(base, k), "required", "field is required"))
        elif e.validator == "additionalProperties" and e.validator_value is False:
            props = e.schema.get("properties", {})
            patterns = [re.compile(p) for p in e.schema.get("patternProperties", {})]
            extras = [k for k in e.instance if k not in props and not any(p.search(k) for p in patterns)]
            for k in sorted(extras):
                out.append(FieldError(entity, _join(base, k), "unknown_field", "field is not defined in the canonical schema"))
        else:
            out.append(FieldError(entity, base, str(e.validator), e.message))
    out.sort(key=lambda x: (x.field, x.code))
    return out


class Schema:
    """One version of the canonical schema."""

    def __init__(self, version: str, raw: Dict[str, Any]):
        self.version = version
        self.raw = raw
        self.id = raw.get("$id", "")
        defs = raw.get("$defs", {})
        self.entities: Dict[str, Entity] = {}
        names = raw.get("properties", {}).get("entities", {}).get("properties", {})
        for name in sorted(names):
            d = defs[name]
            props = {k: v for k, v in d.get("properties", {}).items() if k != META_FIELD}
            self.entities[name] = Entity(name=name, title=d.get("title", ""), template=d.get("x-roi-template", ""),
                                         identity_key=list(d.get("x-identity-key", [])), fields=props)
        self._validators: Dict[str, Draft202012Validator] = {}
        for name in self.entities:
            sub = {"$schema": raw.get("$schema"), "$defs": defs, "$ref": f"#/$defs/{name}"}
            self._validators[name] = Draft202012Validator(sub, format_checker=_FORMATS)

    @property
    def entity_names(self) -> List[str]:
        return sorted(self.entities)

    def entity(self, name: str) -> Optional[Entity]:
        return self.entities.get(name)

    def validate_record(self, entity: str, record: Dict[str, Any]) -> List[FieldError]:
        """L1 structural validation of one record; an empty list means valid."""
        v = self._validators.get(entity)
        if v is None:
            return [FieldError(entity, "", "unknown_entity", f"entity {entity!r} is not defined in canonical schema {self.version}")]
        return errors_of(v, record, entity)


def _read(*parts: str) -> Dict[str, Any]:
    ref = resources.files("compliance_engine_sdk").joinpath("schemas", *parts)
    return json.loads(ref.read_text(encoding="utf-8"))


class Registry:
    """The canonical schema versions this SDK knows."""

    def __init__(self, schemas: Dict[str, Schema]):
        if not schemas:
            raise ValueError("no schema")
        self._by_version = schemas
        self._versions = sorted((Version.parse(v) for v in schemas), key=lambda v: (v.major, v.minor, v.patch))

    @classmethod
    def default(cls) -> "Registry":
        """The schemas shipped with the package."""
        root = resources.files("compliance_engine_sdk").joinpath("schemas")
        schemas = {}
        for entry in root.iterdir():
            if entry.is_dir() and entry.name.startswith("v") and entry.joinpath("schema.json").is_file():
                version = entry.name[1:]
                schemas[version] = Schema(version, json.loads(entry.joinpath("schema.json").read_text(encoding="utf-8")))
        return cls(schemas)

    @property
    def versions(self) -> List[str]:
        return [str(v) for v in self._versions]

    def latest(self) -> Schema:
        return self._by_version[str(self._versions[-1])]

    def resolve(self, version: str) -> Schema:
        """Any patch of a loaded MAJOR.MINOR resolves to its highest loaded patch."""
        want = Version.parse(version)
        best = None
        for v in self._versions:
            if v.major == want.major and v.minor == want.minor:
                best = self._by_version[str(v)]
        if best is None:
            raise UnsupportedVersionError(f"schema version {version} is not supported (supported: {', '.join(self.versions)})")
        return best


_MANIFEST_VALIDATOR: Optional[Draft202012Validator] = None


def validate_manifest_document(doc: Any) -> List[FieldError]:
    """Validates a manifest JSON document (parsed) against the manifest schema."""
    global _MANIFEST_VALIDATOR
    if _MANIFEST_VALIDATOR is None:
        _MANIFEST_VALIDATOR = Draft202012Validator(_read("manifest.schema.json"))
    errs = errors_of(_MANIFEST_VALIDATOR, doc, "")
    for e in errs:
        if e.code == "unknown_field":
            e.message = "field is not defined in the manifest schema"
    return errs
