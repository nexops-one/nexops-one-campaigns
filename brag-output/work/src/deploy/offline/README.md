# compliance-engine offline bundle

This bundle installs compliance-engine on a machine without internet access.
It contains:

| Path | Content |
|---|---|
| `bin/` | the binaries for linux, darwin and windows (amd64, arm64) |
| `images/` | the container image: one `docker load` archive per architecture, and the multi-arch OCI archive |
| `release/` | `SHA256SUMS`, the SBOMs (SPDX) and, for signed releases, the `cosign` signatures and `cosign.pub` |
| `compose.yml`, `.env.example` | Docker Compose deployment using the loaded image |
| `catalogs/`, `docs/` | the built-in catalogs (also embedded in the binary) and the documentation |
| `demo/` | the fictitious sample register and evidence used by the demo |
| `BUNDLE.SHA256SUMS` | checksums of every file in the bundle |

## Install

```bash
tar -xzf compliance-engine_<version>_offline.tar.gz
cd compliance-engine_<version>_offline
./install.sh
```

`install.sh` verifies every checksum (and the signature when `cosign` is
installed), loads the image for the machine's architecture, and prints the
next steps. The PostgreSQL image (`postgres:16-alpine`) is not in the bundle:
load it from your own mirror.

Then follow [docs/install.md](docs/install.md) from step 2.

## Building a bundle

From a checkout of the repository, on a machine with network access:

```bash
VERSION=v0.1.0 deploy/release/release.sh      # binaries, images, SBOMs, checksums (COSIGN_KEY to sign)
deploy/offline/build.sh v0.1.0                 # dist/v0.1.0/compliance-engine_v0.1.0_offline.tar.gz
```
