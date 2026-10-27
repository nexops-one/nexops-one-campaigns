#!/usr/bin/env bash
# Installs compliance-engine from an unpacked offline bundle: verifies the
# checksums (and the cosign signature when cosign and cosign.pub are
# available), loads the image for this machine's architecture into Docker,
# and prints the next steps. It changes nothing else.
set -euo pipefail
cd "$(dirname "$0")"

echo "== verifying the bundle"
sha256sum --quiet -c BUNDLE.SHA256SUMS
for f in bin/* images/* release/sbom-*.spdx.json; do
  [ -e "$f" ] || continue
  base="$(basename "$f")"
  # sha256sum marks binary-mode entries with "*" on some platforms.
  want="$(awk -v n="$base" '{f = $2; sub(/^\*/, "", f)} f == n {print $1}' release/SHA256SUMS)"
  [ -n "$want" ] || { echo "$base is not listed in release/SHA256SUMS"; exit 1; }
  [ "$(sha256sum "$f" | cut -d' ' -f1)" = "$want" ] || { echo "$base does not match release/SHA256SUMS"; exit 1; }
done
if [ -f release/SHA256SUMS.sig ] && [ -f release/cosign.pub ]; then
  if command -v cosign >/dev/null 2>&1; then
    cosign verify-blob --insecure-ignore-tlog=true --key release/cosign.pub --signature release/SHA256SUMS.sig release/SHA256SUMS
  else
    echo "cosign is not installed: the signature in release/SHA256SUMS.sig was not checked"
  fi
else
  echo "this release is not signed"
fi

case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "unsupported architecture $(uname -m)"; exit 1 ;;
esac
image="$(ls images/*_image_linux_${arch}.docker.tar 2>/dev/null | head -n1 || true)"
if [ -n "$image" ] && command -v docker >/dev/null 2>&1; then
  echo "== loading $image"
  docker load -i "$image"
else
  echo "docker or the $arch image is missing: use the binary in bin/ instead"
fi

cat <<'EOF'

== next steps
1. cp .env.example .env and fill it in (POSTGRES_PASSWORD, COMPLIANCE_KEK_FILE, COMPLIANCE_CONSOLE_TENANT).
2. Create the key-encryption key: bin/compliance-engine_<version>_linux_<arch> keys generate --out secrets/compliance-kek
   Keep a copy in your secret store: backups cannot be restored without it.
3. docker compose up -d   (the postgres:16-alpine image must be available locally)
4. Create the first admin user:
   docker compose exec engine /usr/local/bin/compliance-engine user create --tenant <t> --email <e> --workspace <w> --role admin
5. Open http://localhost:8080/console/ behind your TLS reverse proxy.
Documentation: docs/install.md, docs/upgrade.md, docs/backup-restore.md.
EOF
