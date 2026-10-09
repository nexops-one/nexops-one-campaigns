#!/usr/bin/env bash
# End-to-end check of the single-command demo: start deploy/demo/compose.yml,
# read the demo users' passwords from the engine log, run the walkthrough
# (demo steps 1 to 5) against the stack, check that a restart keeps the
# workspace and prints no new passwords, and tear everything down.
# Requires docker (with compose) and go.
set -euo pipefail
cd "$(dirname "$0")"

project=ce-demo-smoke
export COMPLIANCE_DEMO_PORT=18081
base="http://127.0.0.1:${COMPLIANCE_DEMO_PORT}"
compose() { docker compose -p "$project" -f compose.yml "$@"; }
cleanup() { compose down -v >/dev/null 2>&1 || true; }
trap cleanup EXIT

wait_healthy() {
  for _ in $(seq 1 60); do
    curl -fsS "$base/healthz" >/dev/null 2>&1 && return 0
    sleep 1
  done
  echo "the engine did not become healthy"; compose logs engine; exit 1
}

compose up -d --build --wait
wait_healthy

passwords=$(compose logs --no-color engine | sed -nE 's/.* ((owner|approver|auditor|admin)@demo\.invalid) +[a-z+]+ +password: ([^ ]+).*/\1=\3/p' | paste -sd, -)
if [ "$(printf '%s' "$passwords" | tr ',' '\n' | grep -c '=')" != 4 ]; then
  echo "expected four demo passwords in the engine log"; compose logs engine; exit 1
fi

curl -fsS "$base/console/signin" | grep -q 'Sign in' || { echo "the console sign-in page is not served"; exit 1; }

COMPLIANCE_DEMO_URL="$base" COMPLIANCE_DEMO_PASSWORDS="$passwords" go test -count=1 -run TestWalkthrough .

compose restart engine
wait_healthy
compose logs --no-color engine | grep -q 'is already prepared' || { echo "a restart must not bootstrap again"; exit 1; }
echo "demo smoke test passed"
