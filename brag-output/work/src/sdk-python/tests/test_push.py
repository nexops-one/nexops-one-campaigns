# SPDX-License-Identifier: Apache-2.0
"""The push client against a local HTTP server."""

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

from compliance_engine_sdk import (APIError, BatchBuilder, Client, FullBatchTooLarge, Manifest, Mode, PushError,
                                   RecordTooLarge)
from compliance_engine_sdk.push import split


class Engine:
    """Records requests; answers from a script of (status, body), else 201."""

    def __init__(self):
        self.requests = []
        self.script = []
        outer = self

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def _serve(self):
                body = self.rfile.read(int(self.headers.get("Content-Length") or 0))
                outer.requests.append((self.command, self.path, self.headers.get("Authorization"), body))
                status, payload = outer.script.pop(0) if outer.script else (201, None)
                if payload is None and self.path == "/api/v1/ingestions":
                    doc = json.loads(body)
                    n = sum(len(v) for v in doc["entities"].values())
                    payload = {"ingestion_id": "ing-%d" % len(outer.requests), "batch_id": doc.get("batch", {}).get("batch_id", ""),
                               "accepted": n, "snapshot_id": "rev-1", "errors": [], "warnings": [], "created": n}
                data = json.dumps(payload or {}).encode()
                self.send_response(status)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            do_PUT = _serve
            do_POST = _serve

        self.server = HTTPServer(("127.0.0.1", 0), Handler)
        self.url = "http://127.0.0.1:%d" % self.server.server_port
        threading.Thread(target=self.server.serve_forever, daemon=True).start()


@pytest.fixture
def engine():
    e = Engine()
    yield e
    e.server.shutdown()


M = Manifest(name="vendor inventory", version="1.0.0", schema_version="0.1.0",
             supplies={"ict_provider": ["provider_id_code", "legal_name"]}, modes=[Mode.FULL, Mode.INCREMENTAL])


def batch(n, mode=Mode.INCREMENTAL, name_len=10):
    b = BatchBuilder(M, "s").mode(mode).batch_id("b")
    for i in range(n):
        b.add("ict_provider", {"provider_id_code": "P%04d" % i, "legal_name": "x" * name_len})
    return b.build()


def no_wait(seconds):
    pass


def test_register_and_push(engine):
    c = Client(engine.url, "t0k")
    c.register_manifest(M)
    res = c.push(batch(3))
    method, path, auth, body = engine.requests[0]
    assert (method, path, auth) == ("PUT", "/api/v1/adapters/vendor%20inventory/manifest", "Bearer t0k")
    assert json.loads(body)["name"] == "vendor inventory"
    assert len(res) == 1 and res[0].accepted == 3 and res[0].snapshot_id == "rev-1"


def test_incremental_batches_are_split(engine):
    res = Client(engine.url, "t", max_records=4).push(batch(10))
    assert [r.accepted for r in res] == [4, 4, 2]
    assert [r.batch_id for r in res] == ["b#1", "b#2", "b#3"]
    sent = [json.loads(r[3]) for r in engine.requests]
    assert [p["provider_id_code"] for s in sent for p in s["entities"]["ict_provider"]] == ["P%04d" % i for i in range(10)]
    assert all(s["source"]["adapter"] == "vendor inventory" and s["batch"]["mode"] == "incremental" for s in sent)


def test_split_by_size():
    parts = split(batch(20, name_len=200), max_bytes=1500, max_records=1000)
    assert len(parts) > 1 and all(len(p) <= 1500 for p in parts)
    assert sum(len(json.loads(p)["entities"]["ict_provider"]) for p in parts) == 20


def test_full_batches_are_never_split():
    with pytest.raises(FullBatchTooLarge):
        split(batch(10, Mode.FULL), max_bytes=1 << 20, max_records=4)
    assert len(split(batch(4, Mode.FULL), max_bytes=1 << 20, max_records=4)) == 1


def test_record_too_large():
    with pytest.raises(RecordTooLarge):
        split(batch(2, name_len=5000), max_bytes=2000, max_records=1000)


def test_retries_then_succeeds(engine):
    waits = []
    engine.script = [(503, {"error": {"code": "unavailable", "message": "busy"}}),
                     (429, {"error": {"code": "rate_limited", "message": "slow"}})]
    res = Client(engine.url, "t", backoff=0.5, sleep=waits.append).push(batch(1))
    assert res[0].accepted == 1 and waits == [0.5, 1.0] and len(engine.requests) == 3


def test_gives_up_and_reports(engine):
    engine.script = [(503, {"error": {"code": "unavailable", "message": "busy"}})] * 4
    with pytest.raises(APIError) as err:
        Client(engine.url, "t", sleep=no_wait).push(batch(1))
    assert err.value.status == 503 and len(engine.requests) == 4


def test_client_errors_are_not_retried(engine):
    engine.script = [(422, {"error": {"code": "invalid_batch", "message": "bad", "details": [{"field": "x", "code": "required"}]}})]
    with pytest.raises(APIError) as err:
        Client(engine.url, "t", sleep=no_wait).push(batch(1))
    assert (err.value.status, err.value.code, err.value.details[0]["code"]) == (422, "invalid_batch", "required")
    assert len(engine.requests) == 1


def test_partial_results_on_failure(engine):
    engine.script = [(201, None), (400, {"error": {"code": "invalid_json", "message": "no"}})]
    with pytest.raises(APIError) as err:
        Client(engine.url, "t", max_records=2, sleep=no_wait).push(batch(4))
    assert len(err.value.results) == 1


def test_connection_errors_are_retried():
    waits = []
    with pytest.raises(PushError):
        Client("http://127.0.0.1:9", "t", max_attempts=2, sleep=waits.append, timeout=2).push(batch(1))
    assert waits == [0.5]
