#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Lints the Helm chart and validates its rendered manifests. helm is required;
# kubeconform is used when installed (go install github.com/yannh/kubeconform/cmd/kubeconform@v0.6.7).
set -euo pipefail

cd "$(dirname "$0")/.."
chart=deploy/helm/compliance-engine
k8s="${KUBERNETES_VERSION:-1.30.0}"

helm lint --strict "$chart" --set database.urlSecret.name=pg

sets=(
  "--set database.urlSecret.name=pg"
  "--set database.urlSecret.name=pg --set tokens.secret.name=tok --set encryption.kekSecret.name=kek --set license.secret.name=lic --set ingress.enabled=true --set podDisruptionBudget.enabled=true --set networkPolicy.enabled=true --set replicaCount=2"
  "--set database.memory=true --set migrations.enabled=false"
)
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
for i in "${!sets[@]}"; do
  # shellcheck disable=SC2086
  helm template ce "$chart" ${sets[$i]} >"$tmp/$i.yaml"
done
if command -v kubeconform >/dev/null 2>&1; then
  kubeconform -strict -summary -kubernetes-version "$k8s" "$tmp"/*.yaml
else
  echo "kubeconform not installed: rendered manifests not validated against the Kubernetes schemas"
fi
echo "helm chart ok"
