#!/usr/bin/env bash
# Packs a release directory (deploy/release/release.sh) into one offline
# bundle, dist/<version>/compliance-engine_<version>_offline.tar.gz, with the
# binaries, the images, the Compose files, the catalogs, the demo data, the
# documentation and the checksums, plus install.sh.
# Usage: deploy/offline/build.sh [<version>]   (default: git describe)
set -euo pipefail
cd "$(dirname "$0")/../.."

VERSION="${1:-${VERSION:-$(git describe --tags --match 'v[0-9]*' --always --dirty)}}"
rel="dist/$VERSION"
name="compliance-engine_${VERSION}_offline"
[ -f "$rel/SHA256SUMS" ] || { echo "no release in $rel: run deploy/release/release.sh first"; exit 1; }

stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT
dir="$stage/$name"
mkdir -p "$dir/bin" "$dir/images" "$dir/release"

# Repository paths copied into the bundle (checked by TestOfflineBundleFiles).
# BEGIN INCLUDE
include=(
  LICENSE
  NOTICE
  SECURITY.md
  README.md
  catalogs
  docs
  demo/sample-register.xlsx
  demo/evidence
  deploy/offline/README.md
  deploy/offline/install.sh
  deploy/offline/compose.yml
  deploy/.env.example
)
# END INCLUDE
for p in "${include[@]}"; do
  mkdir -p "$dir/$(dirname "$p")"
  cp -R "$p" "$dir/$p"
done
mv "$dir/deploy/offline/README.md" "$dir/deploy/offline/install.sh" "$dir/deploy/offline/compose.yml" "$dir/"
rmdir "$dir/deploy/offline"
mv "$dir/deploy/.env.example" "$dir/.env.example"
rmdir "$dir/deploy"
printf '\n# Image version loaded by install.sh.\nCOMPLIANCE_VERSION=%s\n# Key-encryption key file (bin/compliance-engine keys generate --out ...).\nCOMPLIANCE_KEK_FILE=./secrets/compliance-kek\n# Tenant the console signs users into.\nCOMPLIANCE_CONSOLE_TENANT=\n' "$VERSION" >> "$dir/.env.example"

for f in "$rel"/*; do
  base="$(basename "$f")"
  case "$base" in
    *_offline.tar.gz*) ;;
    *_image_*.docker.tar|*_image.oci.tar) cp "$f" "$dir/images/" ;;
    compliance-engine_*) cp "$f" "$dir/bin/" ;;
    *) cp "$f" "$dir/release/" ;;
  esac
done

(cd "$dir" && find . -type f ! -name BUNDLE.SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > BUNDLE.SHA256SUMS)
tar -C "$stage" -czf "$rel/$name.tar.gz" "$name"
(cd "$rel" && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256")
echo "wrote $rel/$name.tar.gz"
