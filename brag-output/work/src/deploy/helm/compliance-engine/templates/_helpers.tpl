{{/* SPDX-License-Identifier: Apache-2.0 */}}

{{- define "ce.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "ce.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "ce.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "ce.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "ce.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ce.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "ce.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- .Values.serviceAccount.name | default (include "ce.fullname" .) -}}
{{- else -}}
{{- .Values.serviceAccount.name | default "default" -}}
{{- end -}}
{{- end -}}

{{- define "ce.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}

{{/* ce.validate stops rendering on an inconsistent configuration. */}}
{{- define "ce.validate" -}}
{{- if and (not .Values.database.memory) (not .Values.database.urlSecret.name) -}}
{{- fail "database.urlSecret.name is required: create a Secret holding the PostgreSQL URL (or set database.memory=true for a trial)" -}}
{{- end -}}
{{- if and .Values.database.memory (gt (int .Values.replicaCount) 1) -}}
{{- fail "database.memory=true keeps data in one process: it cannot run with replicaCount > 1" -}}
{{- end -}}
{{- if and .Values.database.memory .Values.migrations.enabled -}}
{{- fail "database.memory=true has no schema to migrate: set migrations.enabled=false" -}}
{{- end -}}
{{- range .Values.extraEnv -}}
{{- if and (regexMatch "(?i)(TOKEN|PASSWORD|SECRET|DATABASE_URL)" .name) (not (hasSuffix "_FILE" .name)) -}}
{{- if hasKey . "value" -}}
{{- fail (printf "extraEnv %s looks like a secret: reference a Secret with extraEnvFrom or valueFrom instead of a value" .name) -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* ce.env is the environment shared by the engine and the migration Job. */}}
{{- define "ce.env" -}}
{{- if .Values.database.memory }}
- name: COMPLIANCE_DATABASE_URL
  value: ""
{{- else }}
- name: COMPLIANCE_DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ .Values.database.urlSecret.name }}
      key: {{ .Values.database.urlSecret.key }}
{{- end }}
{{- if .Values.encryption.kekSecret.name }}
- name: COMPLIANCE_ENCRYPTION_KEY_FILE
  value: /etc/compliance/kek/{{ .Values.encryption.kekSecret.key }}
{{- end }}
- name: COMPLIANCE_LOG_LEVEL
  value: {{ .Values.logLevel | quote }}
{{- end -}}

{{- define "ce.secretVolumes" -}}
{{- if .Values.encryption.kekSecret.name }}
- name: kek
  secret:
    secretName: {{ .Values.encryption.kekSecret.name }}
    defaultMode: 0440 # readable by the engine's group (podSecurityContext.fsGroup), not world
    items:
      - key: {{ .Values.encryption.kekSecret.key }}
        path: {{ .Values.encryption.kekSecret.key }}
{{- end }}
{{- end -}}

{{- define "ce.secretMounts" -}}
{{- if .Values.encryption.kekSecret.name }}
- name: kek
  mountPath: /etc/compliance/kek
  readOnly: true
{{- end }}
{{- end -}}
