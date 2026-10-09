#!/usr/bin/env bash
# End-to-end check of the Docker Compose deployment: build, start, register a
# manifest, ingest the sample batch, evaluate DORA, generate and verify a
# Profile B report, tear down.
# Requires docker (with compose), curl and go.
set -euo pipefail
cd "$(dirname "$0")"

gen=$(cd .. && go run ./cmd/compliance-engine token generate --tenant smoke --workspace default)
token=$(printf '%s\n' "$gen" | sed -n 's/^token: //p')
export COMPLIANCE_TOKENS=$(printf '%s\n' "$gen" | sed -n 's/^COMPLIANCE_TOKENS entry: //p')
export POSTGRES_PASSWORD="smoke$(date +%s)"
export COMPLIANCE_HOST_PORT=18080
project=ce-smoke
base="http://127.0.0.1:${COMPLIANCE_HOST_PORT}"
out=$(mktemp)

cleanup() { docker compose -p "$project" down -v >/dev/null 2>&1 || true; rm -f "$out"; }
trap cleanup EXIT

docker compose -p "$project" up -d --build --wait
for _ in $(seq 1 30); do
  curl -fsS "$base/healthz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$base/healthz"; echo
curl -fsS -H "Authorization: Bearer $token" "$base/api/v1/about"; echo

curl -fsS -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -X PUT \
  --data @smoke/manifest.json "$base/api/v1/adapters/csv-import/manifest" >/dev/null

status=$(curl -sS -o "$out" -w '%{http_code}' -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
  -X POST --data @../pkg/compliance/testdata/sample-batch.json "$base/api/v1/ingestions")
if [ "$status" != 201 ]; then echo "ingestion returned HTTP $status"; cat "$out"; exit 1; fi

curl -fsS -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -X POST \
  --data '{"catalogs":["dora@1.0.0"]}' "$base/api/v1/evaluations" > "$out"
grep -q '"coverage_pct":45' "$out" || { echo "unexpected evaluation:"; cat "$out"; exit 1; }

curl -fsS -H "Authorization: Bearer $token" "$base/api/v1/audit?verify=true" > "$out"
grep -q '"action":"ingestion.commit"' "$out" && grep -q '"ok":true' "$out" || { echo "unexpected audit log:"; cat "$out"; exit 1; }

curl -fsS -H "Authorization: Bearer $token" "$base/api/v1/status?catalogs=dora@1.0.0" > "$out"
grep -q '"coverage_pct":45' "$out" && grep -q '"computed_status":"monitoring"' "$out" || { echo "unexpected status:"; cat "$out"; exit 1; }

curl -fsS -H "Authorization: Bearer $token" -H 'Content-Type: application/json' -X POST \
  --data '{"profile":"profile_b","catalogs":["dora@1.0.0"]}' "$base/api/v1/reports" > "$out"
report=$(sed -n 's/.*"id":"\(rpt-[0-9a-f]*\)".*/\1/p' "$out")
[ -n "$report" ] || { echo "unexpected report:"; cat "$out"; exit 1; }
curl -fsS -H "Authorization: Bearer $token" -o "$out" "$base/api/v1/reports/$report/files/report.pdf"
[ "$(head -c 5 "$out")" = "%PDF-" ] || { echo "report.pdf is not a PDF"; exit 1; }
curl -fsS -H "Authorization: Bearer $token" -X POST "$base/api/v1/reports/$report/regenerate" > "$out"
grep -q '"identical":true' "$out" || { echo "report does not regenerate identically:"; cat "$out"; exit 1; }

if curl -fsS "$base/api/v1/about" >/dev/null 2>&1; then echo "unauthenticated request was accepted"; exit 1; fi
echo "smoke test passed"
