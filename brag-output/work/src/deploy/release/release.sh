#!/usr/bin/env bash
# Builds the release artifacts of one version into dist/<version>:
#   - binaries for linux, darwin and windows on amd64 and arm64;
#   - the container image for linux/amd64 and linux/arm64, as a multi-arch
#     OCI archive and as one docker archive per architecture (docker load);
#   - SPDX SBOMs of the source tree and of each image architecture (syft);
#   - SHA256SUMS over all of them;
#   - cosign signatures of SHA256SUMS and the SBOMs when COSIGN_KEY is set.
# Nothing is pushed anywhere.
#
# Environment:
#   VERSION      release version (default: git describe)
#   SKIP_IMAGE=1 build binaries, SBOM of the source and checksums only
#   COSIGN_KEY   cosign private key file; COSIGN_PASSWORD its password
#   SYFT_IMAGE, COSIGN_IMAGE  container images used when syft or cosign are
#                not installed (defaults below)
# Requires go, git, sha256sum; docker with buildx for the image and for
# syft/cosign when they are not installed.
set -euo pipefail
cd "$(dirname "$0")/../.."
# Paths given to docker -v: Windows form under Git Bash, unchanged elsewhere.
export MSYS_NO_PATHCONV=1
hostpath() { (cd "$1" && (pwd -W 2>/dev/null || pwd)); }
root="$(hostpath .)"

VERSION="${VERSION:-$(git describe --tags --match 'v[0-9]*' --always --dirty)}"
out="dist/$VERSION"
SYFT_IMAGE="${SYFT_IMAGE:-anchore/syft:v1.18.1}"
COSIGN_IMAGE="${COSIGN_IMAGE:-gcr.io/projectsigstore/cosign:v2.4.1}"
name="compliance-engine_${VERSION}"
# Installed tools, looked up before the wrappers below shadow their names.
syft_bin="$(command -v syft || true)"
cosign_bin="$(command -v cosign || true)"

rm -rf "$out"
mkdir -p "$out"
echo "== compliance-engine $VERSION -> $out"

echo "== binaries"
for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os="${target%/*}" arch="${target#*/}" ext=""
  [ "$os" = windows ] && ext=".exe"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags "-s -w -X main.version=$VERSION" \
    -o "$out/${name}_${os}_${arch}${ext}" ./cmd/compliance-engine
  echo "   ${name}_${os}_${arch}${ext}"
done

syft() {
  if [ -n "$syft_bin" ]; then
    "$syft_bin" "$@"
  else
    docker run --rm -v "$root:/work" -w /work "$SYFT_IMAGE" "$@"
  fi
}

if [ "${SKIP_IMAGE:-}" != 1 ]; then
  echo "== image (linux/amd64, linux/arm64)"
  builder=ce-release
  docker buildx inspect "$builder" >/dev/null 2>&1 || docker buildx create --name "$builder" --driver docker-container >/dev/null
  common=(--builder "$builder" -f deploy/Dockerfile --build-arg "VERSION=$VERSION" --provenance=false)
  docker buildx build "${common[@]}" --platform linux/amd64,linux/arm64 \
    --output "type=oci,dest=$out/${name}_image.oci.tar" .
  for arch in amd64 arm64; do
    docker buildx build "${common[@]}" --platform "linux/$arch" -t "compliance-engine:$VERSION" \
      --output "type=docker,dest=$out/${name}_image_linux_${arch}.docker.tar" .
  done
fi

echo "== SBOM"
syft "dir:." -q --exclude ./dist --exclude ./bin -o "spdx-json=$out/sbom-source.spdx.json"
# syft reads single-image archives: one image SBOM per architecture.
for arch in amd64 arm64; do
  img="$out/${name}_image_linux_${arch}.docker.tar"
  [ -f "$img" ] || continue
  syft "docker-archive:$img" -q -o "spdx-json=$out/sbom-image-linux-${arch}.spdx.json"
done

echo "== checksums"
(cd "$out" && sha256sum -- * > SHA256SUMS)

if [ -n "${COSIGN_KEY:-}" ]; then
  echo "== signatures (cosign, key-based, no transparency log upload)"
  cosign() {
    if [ -n "$cosign_bin" ]; then
      "$cosign_bin" "$@"
    else
      docker run --rm -e COSIGN_PASSWORD -v "$root:/work" -v "$(hostpath "$(dirname "$COSIGN_KEY")"):/keys:ro" -w /work \
        "$COSIGN_IMAGE" "$@"
    fi
  }
  key="$COSIGN_KEY"
  [ -n "$cosign_bin" ] || key="/keys/$(basename "$COSIGN_KEY")"
  for f in SHA256SUMS $(cd "$out" && ls sbom-*.spdx.json); do
    cosign sign-blob --yes --tlog-upload=false --key "$key" --output-signature "$out/$f.sig" "$out/$f" >/dev/null
    echo "   $f.sig"
  done
  cosign public-key --key "$key" > "$out/cosign.pub"
else
  echo "== COSIGN_KEY is not set: the artifacts are NOT signed"
fi

echo "== done: $out"
ls -l "$out"
