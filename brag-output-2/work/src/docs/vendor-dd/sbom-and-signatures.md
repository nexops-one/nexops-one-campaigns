Status: pending legal review

# Verifying a release: checksums, signatures and SBOM

Every release of the open core is built by `deploy/release/release.sh` from a
tagged commit of the public repository. It ships, per version:

| File | Content |
|---|---|
| binaries and container images (`*.docker.tar`, `*.oci.tar`) | the software |
| `SHA256SUMS` | the SHA-256 of every file of the release |
| `sbom-source.spdx.json`, `sbom-image-linux-<arch>.spdx.json` | SPDX software bills of materials of the source tree and of each image, made with syft |
| `SHA256SUMS.sig`, `sbom-*.spdx.json.sig`, `cosign.pub` | cosign signatures (key-based, signed offline, no transparency log) and the vendor's public key |

## 1. Check that you have the vendor's key

Compare `cosign.pub` with the key the vendor published through a second
channel (TO BE COMPLETED: where the vendor publishes its release signing key
and its fingerprint). Do not trust a key that only came with the files.

## 2. Verify the signatures, then the files

```bash
cosign verify-blob --insecure-ignore-tlog=true --key cosign.pub --signature SHA256SUMS.sig SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
for f in sbom-*.spdx.json; do
  cosign verify-blob --insecure-ignore-tlog=true --key cosign.pub --signature "$f.sig" "$f"
done
```

`--insecure-ignore-tlog=true` is needed because releases are signed offline:
no transparency-log entry exists to check. The signature itself is fully
verified.

A verified `SHA256SUMS` vouches for every binary and image archive. After
`docker load -i compliance-engine_<version>_image_linux_amd64.docker.tar`, the
image is the one that was signed.

## 3. Read the SBOM

The SBOMs are SPDX 2.3 JSON. List the components and licenses with any SPDX
tool, for example:

```bash
jq -r '.packages[] | [.name, .versionInfo, .licenseConcluded] | @tsv' sbom-image-linux-amd64.spdx.json | sort -u
grype sbom:sbom-image-linux-amd64.spdx.json      # known vulnerabilities, with a scanner of your choice
```

The dependency policy is permissive licenses only; the open core's direct Go
dependencies are listed in `go.mod`.

## 4. Rebuild it yourself (open core)

The open core builds from source with Go 1.24 (`go build ./cmd/compliance-engine`),
or with the release script (`-trimpath`, pinned toolchain). Byte-identical
builds are not guaranteed yet; compare your build's SBOM with the published one.

## Enterprise edition

The enterprise image is built from a private repository with the open core as
a named build context. Its releases will follow the same pattern (checksums,
SBOM, signatures); TO BE COMPLETED when the first commercial release is made.
