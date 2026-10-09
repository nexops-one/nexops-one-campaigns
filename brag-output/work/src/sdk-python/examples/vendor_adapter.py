# SPDX-License-Identifier: Apache-2.0
"""Example adapter: vendor inventory.

The Python twin of examples/adapter-go: it maps the JSON export of a
fictional vendor inventory system to canonical ict_provider and
cloud_resource records, using only the SDK.

    python vendor_adapter.py                      # print the batch
    python vendor_adapter.py --out ./out          # write out/manifest.json and out/batches/vendors.json
    COMPLIANCE_TOKEN=... python vendor_adapter.py --engine http://localhost:8080   # register and push

Check the written files with the engine's language-neutral runner:

    compliance-engine adapter test --manifest out/manifest.json --batches out/batches --derived cloud_resource.country
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from typing import Dict, List

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))  # the SDK of this repository

from compliance_engine_sdk import (APIError, Batch, BatchBuilder, Client, Manifest, Mode, PushError,  # noqa: E402
                                   mark_derived, set_source_ref)

# Cloud regions and the country hosting them. A region that is not listed
# leaves the country missing: the adapter never guesses.
REGION_COUNTRY = {"eu-west-1": "IE", "eu-west-3": "FR", "eu-central-1": "DE"}

MANIFEST = Manifest(
    name="vendor-inventory", version="1.0.0", schema_version="0.1.0",
    supplies={
        "ict_provider": ["provider_id_code", "provider_id_type", "legal_name", "hq_country"],
        "cloud_resource": ["resource_ref", "provider_id_code", "service_name", "region", "country"],
    },
    modes=[Mode.FULL, Mode.INCREMENTAL],
)


def _set(rec: Dict, field: str, value: str) -> None:
    """Adds a field only when the source has a value: absent stays missing."""
    if value:
        rec[field] = value


def pull(path: str, mode: str = Mode.FULL) -> Batch:
    """Maps the whole export. It has no change tracking, so an incremental
    pull sends every record too; nothing is deleted in that mode."""
    with open(path, encoding="utf-8") as f:
        export = json.load(f)
    b = BatchBuilder(MANIFEST, "vendor-inventory-export").mode(mode).batch_id("vendors@" + export["exported_at"])
    for v in export["vendors"]:
        code = v.get("lei") or "vendor:" + v["id"]
        p = {"provider_id_code": code}
        if v.get("lei"):
            p["provider_id_type"] = "LEI"
        _set(p, "legal_name", v.get("name", ""))
        _set(p, "hq_country", v.get("country", ""))
        set_source_ref(p, "vendors/" + v["id"])
        b.add("ict_provider", p)
        for r in v.get("resources", []):
            rec = {"resource_ref": v["id"] + "/" + r["id"], "provider_id_code": code}
            _set(rec, "service_name", r.get("service", ""))
            _set(rec, "region", r.get("region", ""))
            country = REGION_COUNTRY.get(r.get("region", ""))
            if country:
                rec["country"] = country
                mark_derived(rec, "country", "region_lookup", r["region"])
            set_source_ref(rec, "vendors/" + v["id"] + "/resources/" + r["id"])
            b.add("cloud_resource", rec)
    return b.build()


def write_files(out: str, batch: Batch) -> List[str]:
    os.makedirs(os.path.join(out, "batches"), exist_ok=True)
    paths = []
    for path, doc in ((os.path.join(out, "manifest.json"), MANIFEST.to_dict()),
                      (os.path.join(out, "batches", "vendors.json"), batch.to_dict())):
        with open(path, "w", encoding="utf-8", newline="\n") as f:
            f.write(json.dumps(doc, indent=2, ensure_ascii=False) + "\n")
        paths.append(path)
    return paths


def main(argv: List[str]) -> int:
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--input", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), "testdata", "vendors.json"))
    ap.add_argument("--mode", default=Mode.FULL, choices=Mode.ALL)
    ap.add_argument("--out", help="write manifest.json and batches/vendors.json to this directory")
    ap.add_argument("--engine", help="push to the engine at this base URL, with the token in COMPLIANCE_TOKEN")
    args = ap.parse_args(argv)
    batch = pull(args.input, args.mode)
    if args.out:
        print("wrote " + " and ".join(write_files(args.out, batch)))
    elif args.engine:
        client = Client(args.engine, os.environ.get("COMPLIANCE_TOKEN", ""))
        try:
            client.register_manifest(MANIFEST)
            for r in client.push(batch):
                print(f"ingestion {r.ingestion_id}: accepted {r.accepted}, rejected {r.rejected_records}, snapshot {r.snapshot_id}")
                for e in r.errors:
                    print(f"  {e.entity}[{e.index}] {e.field}: {e.message} ({e.code})")
        except (APIError, PushError) as e:
            print("error:", e, file=sys.stderr)
            return 1
    else:
        print(batch.to_json(indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
