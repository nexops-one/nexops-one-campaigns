# SPDX-License-Identifier: Apache-2.0
"""Python SDK of the compliance engine: build canonical batches, validate them
locally against the canonical schemas, and push them to an engine.

It mirrors the Go SDK (github.com/nexops-one/compliance-engine/sdk).
"""

from .adapter import (
    Batch,
    BatchBuilder,
    BatchInfo,
    Manifest,
    ManifestError,
    Mode,
    Record,
    Source,
    mark_derived,
    mark_not_applicable,
    meta_of,
    set_source_ref,
    validate_batch,
)
from .push import APIError, Client, FullBatchTooLarge, IngestionResult, PushError, RecordTooLarge
from .schema import FieldError, Registry, Schema, UnsupportedVersionError, validate_manifest_document

__version__ = "0.1.0"

__all__ = [
    "APIError", "Batch", "BatchBuilder", "BatchInfo", "Client", "FieldError", "FullBatchTooLarge", "IngestionResult",
    "Manifest", "ManifestError", "Mode", "PushError", "Record", "RecordTooLarge", "Registry", "Schema", "Source",
    "UnsupportedVersionError", "mark_derived", "mark_not_applicable", "meta_of", "set_source_ref", "validate_batch",
    "validate_manifest_document",
]
