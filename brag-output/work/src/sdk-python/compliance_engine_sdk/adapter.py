# SPDX-License-Identifier: Apache-2.0
"""The adapter contract: manifest, batch, builder and record metadata.

It mirrors the Go SDK's ``adapter`` package. An adapter declares a Manifest
and produces Batches of canonical records; the engine binds the tenant and
workspace (through the API token), never the adapter.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Dict, Iterable, List, Optional

from .schema import META_FIELD, FieldError, Registry, Schema, UnsupportedVersionError

Record = Dict[str, Any]


class Mode:
    """Synchronization modes of a batch."""

    FULL = "full"  # the batch is everything the adapter supplies: its records absent from it are deleted
    INCREMENTAL = "incremental"  # records are upserted; nothing is deleted

    ALL = (FULL, INCREMENTAL)


class ManifestError(ValueError):
    """An invalid manifest; ``problems`` lists every issue."""

    def __init__(self, problems: List[str]):
        super().__init__("; ".join(problems))
        self.problems = problems


@dataclass
class Manifest:
    """An adapter's capability declaration (PUT /api/v1/adapters/{name}/manifest)."""

    name: str
    version: str
    schema_version: str
    supplies: Dict[str, List[str]]
    modes: List[str]

    def to_dict(self) -> Dict[str, Any]:
        return {"name": self.name, "version": self.version, "schema_version": self.schema_version,
                "supplies": {k: list(v) for k, v in self.supplies.items()}, "modes": list(self.modes)}

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Manifest":
        unknown = set(d) - {"name", "version", "schema_version", "supplies", "modes"}
        if unknown:
            raise ManifestError([f"unknown manifest field {k!r}" for k in sorted(unknown)])
        return cls(name=d.get("name", ""), version=d.get("version", ""), schema_version=d.get("schema_version", ""),
                   supplies={k: list(v) for k, v in (d.get("supplies") or {}).items()}, modes=list(d.get("modes") or []))

    def validate(self, registry: Registry) -> None:
        """Checks the manifest against the registry, like the Go SDK: an
        adapter that supplies an entity must supply its identity fields."""
        problems: List[str] = []
        if not self.name.strip():
            problems.append("manifest: name is required")
        if not self.version.strip():
            problems.append("manifest: version is required")
        try:
            s = registry.resolve(self.schema_version)
        except UnsupportedVersionError as e:
            raise ManifestError(problems + [f"manifest: {e}"]) from None
        for entity in sorted(self.supplies):
            fields = self.supplies[entity]
            e = s.entity(entity)
            if e is None:
                problems.append(f"manifest: unknown entity {entity!r}")
                continue
            for f in fields:
                if f not in e.fields:
                    problems.append(f"manifest: unknown field {entity}.{f}")
            for k in e.identity_key:
                if k not in fields:
                    problems.append(f"manifest: {entity} supplies the entity but not its identity field {k}")
        if not self.modes:
            problems.append("manifest: at least one mode is required")
        for m in self.modes:
            if m not in Mode.ALL:
                problems.append(f"manifest: unknown mode {m!r}")
        if problems:
            raise ManifestError(problems)

    def supplies_field(self, entity: str, f: str) -> bool:
        return f in self.supplies.get(entity, [])

    def supports_mode(self, mode: str) -> bool:
        return mode in self.modes


@dataclass
class Source:
    system: str
    adapter: str
    adapter_version: str


@dataclass
class BatchInfo:
    batch_id: str = ""
    generated_at: Optional[str] = None  # RFC 3339
    mode: str = ""


@dataclass
class Batch:
    """The ingestion envelope of the canonical model."""

    schema_version: str
    source: Source
    entities: Dict[str, List[Record]] = field(default_factory=dict)
    batch: Optional[BatchInfo] = None

    @property
    def mode(self) -> str:
        return (self.batch.mode if self.batch else "") or Mode.INCREMENTAL

    @property
    def batch_id(self) -> str:
        return self.batch.batch_id if self.batch else ""

    def record_count(self) -> int:
        return sum(len(r) for r in self.entities.values())

    def to_dict(self) -> Dict[str, Any]:
        out: Dict[str, Any] = {"schema_version": self.schema_version}
        if self.batch is not None:
            info: Dict[str, Any] = {}
            if self.batch.batch_id:
                info["batch_id"] = self.batch.batch_id
            if self.batch.generated_at:
                info["generated_at"] = self.batch.generated_at
            if self.batch.mode:
                info["mode"] = self.batch.mode
            out["batch"] = info
        out["source"] = {"system": self.source.system, "adapter": self.source.adapter, "adapter_version": self.source.adapter_version}
        out["entities"] = self.entities
        return out

    def to_json(self, indent: Optional[int] = None) -> str:
        return json.dumps(self.to_dict(), indent=indent, ensure_ascii=False)

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "Batch":
        """Strict: unknown envelope fields are rejected, like the Go SDK."""
        unknown = set(d) - {"schema_version", "batch", "source", "entities"}
        if unknown:
            raise ValueError(f"decode batch: unknown field {sorted(unknown)[0]!r}")
        info = None
        if d.get("batch") is not None:
            b = d["batch"]
            extra = set(b) - {"batch_id", "generated_at", "mode"}
            if extra:
                raise ValueError(f"decode batch: unknown field batch.{sorted(extra)[0]}")
            info = BatchInfo(batch_id=b.get("batch_id", ""), generated_at=b.get("generated_at"), mode=b.get("mode", ""))
        src = d.get("source") or {}
        extra = set(src) - {"system", "adapter", "adapter_version"}
        if extra:
            raise ValueError(f"decode batch: unknown field source.{sorted(extra)[0]}")
        return cls(schema_version=d.get("schema_version", ""),
                   source=Source(src.get("system", ""), src.get("adapter", ""), src.get("adapter_version", "")),
                   entities=d.get("entities"), batch=info)

    @classmethod
    def from_json(cls, text: str) -> "Batch":
        return cls.from_dict(json.loads(text))

    def validate_envelope(self, schema: Schema) -> List[FieldError]:
        """Checks everything except the records themselves."""
        errs: List[FieldError] = []

        def add(f: str, code: str, msg: str) -> None:
            errs.append(FieldError("", f, code, msg))

        if not re.fullmatch(r"\d+\.\d+\.\d+", self.schema_version or ""):
            add("schema_version", "pattern", "schema_version must be MAJOR.MINOR.PATCH")
        for f, v in (("source.system", self.source.system), ("source.adapter", self.source.adapter),
                     ("source.adapter_version", self.source.adapter_version)):
            if not (v or "").strip():
                add(f, "required", "field is required")
            if "\x00" in (v or ""):
                add(f, "invalid_character", "text must not contain the NUL character (U+0000)")
        if self.batch is not None and "\x00" in self.batch.batch_id:
            add("batch.batch_id", "invalid_character", "text must not contain the NUL character (U+0000)")
        if self.batch is not None and self.batch.mode and self.batch.mode not in Mode.ALL:
            add("batch.mode", "enum", 'mode must be "full" or "incremental"')
        if self.entities is None:
            add("entities", "required", "field is required")
        for name in sorted(self.entities or {}):
            if schema.entity(name) is None:
                add("entities." + name, "unknown_entity", f"entity {name!r} is not defined in canonical schema {schema.version}")
        errs.sort(key=lambda e: e.field)
        return errs


def validate_batch(batch: Batch, registry: Registry) -> List[FieldError]:
    """Validates a batch locally before sending it: the envelope, then each
    record (L1). Record errors carry the entity, the record's index and its
    source_record_ref. The engine runs the same checks and more (identifier
    checks, references, completeness)."""
    try:
        s = registry.resolve(batch.schema_version)
    except UnsupportedVersionError as e:
        return [FieldError("", "schema_version", "unsupported_schema_version", str(e))]
    errs = batch.validate_envelope(s)
    if errs:
        return errs
    for entity in sorted(batch.entities):
        if s.entity(entity) is None:
            continue
        for i, rec in enumerate(batch.entities[entity]):
            ref = meta_of(rec).get("source_record_ref", "")
            for e in s.validate_record(entity, rec):
                e.index, e.source_record_ref = i, ref
                errs.append(e)
    return errs


class BatchBuilder:
    """Assembles a batch for a manifest."""

    def __init__(self, manifest: Manifest, system: str):
        self._batch = Batch(schema_version=manifest.schema_version,
                            source=Source(system=system, adapter=manifest.name, adapter_version=manifest.version), entities={})

    def _info(self) -> BatchInfo:
        if self._batch.batch is None:
            self._batch.batch = BatchInfo()
        return self._batch.batch

    def mode(self, mode: str) -> "BatchBuilder":
        self._info().mode = mode
        return self

    def batch_id(self, batch_id: str) -> "BatchBuilder":
        self._info().batch_id = batch_id
        return self

    def generated_at(self, at: datetime) -> "BatchBuilder":
        if at.tzinfo is None:
            raise ValueError("generated_at needs a timezone")
        self._info().generated_at = at.astimezone(timezone.utc).isoformat().replace("+00:00", "Z")
        return self

    def add(self, entity: str, record: Record) -> "BatchBuilder":
        self._batch.entities.setdefault(entity, []).append(record)
        return self

    def build(self) -> Batch:
        return self._batch


# Record metadata ("_meta").

def meta_of(record: Record) -> Dict[str, Any]:
    """The record's metadata (a copy); malformed metadata reads as empty."""
    m = record.get(META_FIELD)
    if not isinstance(m, dict):
        return {}
    return json.loads(json.dumps(m))


def _set_meta(record: Record, meta: Dict[str, Any]) -> None:
    meta = {k: v for k, v in meta.items() if v not in ("", None, [])}
    if meta:
        record[META_FIELD] = meta
    else:
        record.pop(META_FIELD, None)


def set_source_ref(record: Record, ref: str) -> None:
    """Records the record's identifier in the source system."""
    m = meta_of(record)
    m["source_record_ref"] = ref
    _set_meta(record, m)


def mark_not_applicable(record: Record, *fields: str) -> None:
    """Declares fields that do not apply to this record (distinct from missing)."""
    m = meta_of(record)
    current = list(m.get("not_applicable_fields") or [])
    for f in fields:
        if f not in current:
            current.append(f)
    m["not_applicable_fields"] = current
    _set_meta(record, m)


def mark_derived(record: Record, f: str, method: str, source_ref: str = "") -> None:
    """Declares that a field's value was inferred rather than read."""
    m = meta_of(record)
    derived = [d for d in (m.get("derived_fields") or []) if d.get("field") != f]
    d: Dict[str, Any] = {"field": f, "method": method}
    if source_ref:
        d["source_ref"] = source_ref
    derived.append(d)
    m["derived_fields"] = derived
    _set_meta(record, m)


def iter_records(batch: Batch) -> Iterable[tuple]:
    """Yields (entity, index, record) in entity-name then record order."""
    for entity in sorted(batch.entities):
        for i, rec in enumerate(batch.entities[entity]):
            yield entity, i, rec
