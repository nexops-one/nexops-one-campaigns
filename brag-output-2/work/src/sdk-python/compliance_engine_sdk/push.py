# SPDX-License-Identifier: Apache-2.0
"""Delivery to an engine over its HTTP API, with the standard library only.

It mirrors the Go SDK's ``push`` package: register the manifest, then submit
batches. Large incremental batches are split; full batches never are.
"""

from __future__ import annotations

import json
import time
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import dataclass, field
from typing import Any, Callable, Dict, List, Optional

from .adapter import Batch, Manifest, Mode
from .schema import FieldError

DEFAULT_MAX_BODY_BYTES = 16 << 20  # well below the engine's default 32 MiB body limit
DEFAULT_MAX_RECORDS = 10000  # well below the engine's default 50,000-record limit
DEFAULT_MAX_ATTEMPTS = 4  # attempts per request, the first included
DEFAULT_BACKOFF = 0.5  # seconds before the first retry; doubles on each retry

RETRYABLE = {429, 502, 503, 504}


class PushError(Exception):
    """Base class of the push errors."""


class FullBatchTooLarge(PushError):
    """A full-mode batch exceeds the per-request limits. Full batches are never
    split: each part would delete the records the other parts supplied. Raise
    the engine's limits and the client's, or sync incrementally."""


class RecordTooLarge(PushError):
    """One record alone exceeds the per-request size limit."""


class APIError(PushError):
    """A non-2xx response from the engine."""

    def __init__(self, status: int, code: str, message: str, details: Optional[List[Dict[str, Any]]] = None):
        super().__init__(f"push: engine returned {status} {code}: {message}")
        self.status, self.code, self.message, self.details = status, code, message, details or []


@dataclass
class IngestionResult:
    """The engine's response to one ingestion request."""

    ingestion_id: str = ""
    batch_id: str = ""
    schema_version: str = ""
    mode: str = ""
    accepted: int = 0
    rejected_records: int = 0
    errors: List[FieldError] = field(default_factory=list)
    warnings: List[FieldError] = field(default_factory=list)
    created: int = 0
    updated: int = 0
    deleted: int = 0
    unchanged: int = 0
    no_changes: bool = False
    snapshot_id: str = ""

    @classmethod
    def from_dict(cls, d: Dict[str, Any]) -> "IngestionResult":
        def errs(items: Any) -> List[FieldError]:
            out = []
            for e in items or []:
                out.append(FieldError(entity=e.get("entity", ""), field=e.get("field", ""), code=e.get("code", ""),
                                      message=e.get("message", ""), index=e.get("index", -1),
                                      source_record_ref=e.get("source_record_ref", "")))
            return out

        known = {k: d[k] for k in cls.__dataclass_fields__ if k in d and k not in ("errors", "warnings")}
        return cls(**known, errors=errs(d.get("errors")), warnings=errs(d.get("warnings")))


def _dumps(v: Any) -> bytes:
    return json.dumps(v, ensure_ascii=False, separators=(",", ":")).encode("utf-8")


def split(batch: Batch, max_bytes: int, max_records: int) -> List[bytes]:
    """The request bodies of a batch: one when it fits; otherwise, for an
    incremental batch, parts in entity-name then record order, each keeping
    the source and mode, with "#<n>" appended to the batch ID."""
    whole = _dumps(batch.to_dict())
    n = batch.record_count()
    if len(whole) <= max_bytes and n <= max_records:
        return [whole]
    if batch.mode == Mode.FULL:
        raise FullBatchTooLarge(f"push: full-mode batch exceeds the per-request limits and full batches are never split: "
                                f"{n} records in {len(whole)} bytes, limits {max_records} records and {max_bytes} bytes")
    names = sorted(batch.entities)

    def envelope(part: int, entities: Dict[str, List[bytes]]) -> bytes:
        d = batch.to_dict()
        if "batch" in d and d["batch"].get("batch_id"):
            d["batch"]["batch_id"] = f"{d['batch']['batch_id']}#{part}"
        del d["entities"]
        # The envelope without entities, then the already-encoded records.
        head = _dumps(d)[:-1]
        body = b",".join(_dumps(k) + b":[" + b",".join(v) + b"]" for k, v in entities.items())
        return head + b',"entities":{' + body + b"}}"

    overhead = len(envelope(n, {name: [] for name in names}))
    parts: List[bytes] = []
    cur: Dict[str, List[bytes]] = {}
    size, count = overhead, 0
    for name in names:
        for i, rec in enumerate(batch.entities[name]):
            raw = _dumps(rec)
            add = len(raw) + 1
            if overhead + add > max_bytes:
                raise RecordTooLarge(f"push: a record exceeds the per-request size limit: {name}[{i}] is {len(raw)} bytes, limit {max_bytes}")
            if count > 0 and (size + add > max_bytes or count + 1 > max_records):
                parts.append(envelope(len(parts) + 1, cur))
                cur, size, count = {}, overhead, 0
            cur.setdefault(name, []).append(raw)
            size += add
            count += 1
    if count > 0:
        parts.append(envelope(len(parts) + 1, cur))
    return parts


class Client:
    """Talks to one engine. The token's scope selects the tenant and workspace."""

    def __init__(self, base_url: str, token: str, *, max_body_bytes: int = DEFAULT_MAX_BODY_BYTES,
                 max_records: int = DEFAULT_MAX_RECORDS, max_attempts: int = DEFAULT_MAX_ATTEMPTS,
                 backoff: float = DEFAULT_BACKOFF, timeout: float = 300.0, sleep: Callable[[float], None] = time.sleep):
        self.base_url = base_url.rstrip("/")
        self.token = token
        self.max_body_bytes = max_body_bytes
        self.max_records = max_records
        self.max_attempts = max_attempts
        self.backoff = backoff
        self.timeout = timeout
        self._sleep = sleep

    def register_manifest(self, manifest: Manifest) -> None:
        """Registers (or replaces) the adapter's manifest."""
        self._do("PUT", "/api/v1/adapters/" + urllib.parse.quote(manifest.name, safe="") + "/manifest", _dumps(manifest.to_dict()))

    def push(self, batch: Batch) -> List[IngestionResult]:
        """Submits a batch; returns one result per request. If a request fails,
        the error carries the results already committed in ``results``."""
        results: List[IngestionResult] = []
        for body in split(batch, self.max_body_bytes, self.max_records):
            try:
                results.append(IngestionResult.from_dict(self._do("POST", "/api/v1/ingestions", body)))
            except PushError as e:
                e.results = results  # type: ignore[attr-defined]
                raise
        return results

    def _do(self, method: str, path: str, body: bytes) -> Any:
        attempt = 0
        while True:
            attempt += 1
            req = urllib.request.Request(self.base_url + path, data=body, method=method,
                                         headers={"Content-Type": "application/json", "Accept": "application/json"})
            if self.token:
                req.add_header("Authorization", "Bearer " + self.token)
            retry = True
            try:
                with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                    data = resp.read(8 << 20)
                    return json.loads(data) if data.strip() else None
            except urllib.error.HTTPError as e:
                err = _api_error(e.code, e.read(8 << 20))
                retry = e.code in RETRYABLE
            except (urllib.error.URLError, ConnectionError, TimeoutError) as e:
                err = PushError(f"push: {method} {path}: {e}")
            if not retry or attempt >= self.max_attempts:
                raise err
            self._sleep(self.backoff * (2 ** (attempt - 1)))


def _api_error(status: int, data: bytes) -> APIError:
    try:
        env = json.loads(data)
        e = env.get("error") if isinstance(env, dict) else None
        if isinstance(e, dict):
            return APIError(status, e.get("code", ""), e.get("message", ""), e.get("details"))
    except ValueError:
        pass
    return APIError(status, "", data.decode("utf-8", "replace").strip()[:512])
