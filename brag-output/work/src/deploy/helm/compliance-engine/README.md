# compliance-engine Helm chart

Installs the compliance engine (the open-core image, or the enterprise image)
on Kubernetes 1.25 or later, with PostgreSQL you provide.

The chart never creates a Secret and never takes a secret value: it
references Secrets you create beforehand.

## Install

```bash
kubectl create namespace compliance
# PostgreSQL URL (required)
kubectl -n compliance create secret generic ce-database --from-literal=url='postgres://compliance:...@pg:5432/compliance?sslmode=require'
# Bootstrap token entry from 'compliance-engine token generate' (or create stored tokens later)
kubectl -n compliance create secret generic ce-tokens --from-literal=tokens='<hash>:acme:main'
# Key-encryption key from 'compliance-engine keys generate --out kek' (recommended)
kubectl -n compliance create secret generic ce-kek --from-file=kek=./kek

helm install ce deploy/helm/compliance-engine -n compliance \
  --set image.repository=registry.example.com/compliance-engine --set image.tag=0.1.0 \
  --set database.urlSecret.name=ce-database \
  --set tokens.secret.name=ce-tokens \
  --set encryption.kekSecret.name=ce-kek
```

**Keep a copy of the key-encryption key outside the cluster:** a backup
without its key cannot be restored.

## What it creates

| Object | When |
|---|---|
| Deployment `engine` | always; non-root (uid 65532), read-only root filesystem, all capabilities dropped, `RuntimeDefault` seccomp, `/tmp` as the only writable path; probes on `/healthz` |
| Service (port 8080) | always |
| ServiceAccount | `serviceAccount.create` (default), without an API token |
| Job `migrate` | `migrations.enabled` (default): a `pre-install,pre-upgrade` hook running `compliance-engine migrate up` once; the engine then starts with `COMPLIANCE_AUTO_MIGRATE=false` |
| Ingress | `ingress.enabled` |
| PodDisruptionBudget | `podDisruptionBudget.enabled` |
| NetworkPolicy | `networkPolicy.enabled`: ingress to port 8080 from the namespace (or `ingressFrom`), egress to DNS, the database port and `extraEgress` only |

## Values

| Value | Purpose |
|---|---|
| `image.repository`, `image.tag`, `image.args` | the image (tag defaults to the chart's `appVersion`) |
| `database.urlSecret.{name,key}` | `COMPLIANCE_DATABASE_URL`; required unless `database.memory=true` (trials only, one replica, no migrations) |
| `tokens.secret.{name,key}` | `COMPLIANCE_TOKENS` (bootstrap tokens) |
| `encryption.kekSecret.{name,key}` | mounted file, `COMPLIANCE_ENCRYPTION_KEY_FILE` |
| `license.secret.{name,key}` | mounted file, `COMPLIANCE_LICENSE_FILE` (enterprise image) |
| `retention.interval` | `COMPLIANCE_RETENTION_INTERVAL` |
| `extraEnv`, `extraEnvFrom` | every other `COMPLIANCE_*` variable; a value that looks like a secret is refused in `extraEnv`: use `extraEnvFrom` with a Secret |
| `extraVolumes`, `extraVolumeMounts` | evidence root, catalog directory, catalog feed |

See `values.yaml` for every value and its default, and
`docs/reference/configuration.md` for the variables.

## Several replicas

- PostgreSQL is required.
- Console imports keep the validated file in process memory between the validation and the commit: use sticky sessions on the ingress, or users re-upload.
- Each replica runs the daily retention job; it is idempotent, but you can set `retention.interval: "0"` and run `compliance-engine retention run` from a CronJob instead.

## The enterprise image

```bash
kubectl -n compliance create secret generic ce-license --from-file=license.json
helm upgrade ce deploy/helm/compliance-engine -n compliance --reuse-values \
  --set image.repository=registry.example.com/compliance-engine-enterprise \
  --set license.secret.name=ce-license \
  --set 'extraEnvFrom[0].secretRef.name=ce-enterprise'   # COMPLIANCE_OIDC_*, COMPLIANCE_SYSTEM_ADMIN_TOKENS
```

## Checks

`scripts/check-helm.sh` runs `helm lint --strict` and validates the rendered
manifests with `kubeconform -strict` (when installed); `go test ./deploy`
renders the chart with several value sets when `helm` is installed.
