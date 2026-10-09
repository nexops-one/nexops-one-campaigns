#!/usr/bin/env bash
# Checks that every non-merge commit in a range carries a Developer
# Certificate of Origin sign-off matching its author:
#   Signed-off-by: <author name> <author email>
# Usage: scripts/check-dco.sh [<range>]   (default: 0a6ddf8..HEAD, every
# commit since the policy was adopted; see CONTRIBUTING.md)
set -euo pipefail

range="${1:-0a6ddf8..HEAD}"
fail=0
count=0
for commit in $(git rev-list --no-merges "$range"); do
  count=$((count + 1))
  author="$(git show -s --format='%an <%ae>' "$commit")"
  if ! git show -s --format='%(trailers:key=Signed-off-by,valueonly)' "$commit" | grep -qxF "$author"; then
    echo "missing sign-off: $(git show -s --format='%h %s' "$commit") (expected: Signed-off-by: $author)"
    fail=1
  fi
done
if [ "$fail" -ne 0 ]; then
  echo "Sign off with 'git commit -s' (see CONTRIBUTING.md)."
  exit 1
fi
echo "DCO: $count commits in $range are signed off"
