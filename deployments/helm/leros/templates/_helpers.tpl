{{/*
Expand the name of the chart.
*/}}
{{- define "leros.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this
(by the DNS naming spec). If release name contains chart name it will be used as
a full name.
*/}}
{{- define "leros.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Chart name and version label.
*/}}
{{- define "leros.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels. 接受 root context（.），输出 chart/name/instance/managed-by。
component 由调用方通过手写 `app.kubernetes.io/component: xxx` 提供。
matchLabels 用 selectorLabels（需 dict）以保证只含稳定字段。
*/}}
{{- define "leros.labels" -}}
helm.sh/chart: {{ include "leros.chart" . }}
app.kubernetes.io/name: {{ include "leros.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels. Requires a `ctx` dict with at least `.root` and `.component`.
Usage: {{ include "leros.selectorLabels" (dict "root" . "component" "server") }}
*/}}
{{- define "leros.selectorLabels" -}}
app.kubernetes.io/name: {{ include "leros.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/*
Image pull secret names builder.
*/}}
{{- define "leros.imagePullSecrets" -}}
{{- $pullSecrets := list -}}
{{- if .Values.imagePullSecret.enabled -}}
{{- $pullSecrets = append $pullSecrets .Values.imagePullSecret.name -}}
{{- end -}}
{{- range .Values.imagePullSecrets -}}
{{- $pullSecrets = append $pullSecrets .name -}}
{{- end -}}
{{- if $pullSecrets }}
imagePullSecrets:
{{- range $pullSecrets }}
  - name: {{ . }}
{{- end }}
{{- end -}}
{{- end -}}

{{/*
Fully qualified server service name (worker 回连地址用).
*/}}
{{- define "leros.serverServiceName" -}}
{{- if .Values.server.serviceNameOverride -}}
{{- .Values.server.serviceNameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- include "leros.fullname" . | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Worker ConfigMap name (scheduler.config_map).
*/}}
{{- define "leros.workerConfigMapName" -}}
{{- if .Values.worker.configMapNameOverride -}}
{{- .Values.worker.configMapNameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-worker-config" (include "leros.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Worker Secret name (scheduler.secret), key LLM_API_KEY.
*/}}
{{- define "leros.workerSecretName" -}}
{{- if .Values.worker.secretNameOverride -}}
{{- .Values.worker.secretNameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-secret" (include "leros.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Server ConfigMap name (含 config.yaml).
*/}}
{{- define "leros.serverConfigMapName" -}}
{{- if .Values.server.configMapNameOverride -}}
{{- .Values.server.configMapNameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-server-config" (include "leros.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Server Secret name.
*/}}
{{- define "leros.serverSecretName" -}}
{{- printf "%s-server-secret" (include "leros.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Build database URL. Uses builtin postgresql when enabled, otherwise external.url.
*/}}
{{- define "leros.databaseURL" -}}
{{- if .Values.postgresql.enabled -}}
{{- printf "postgres://%s:%s@%s-postgresql:5432/%s?sslmode=disable" .Values.postgresql.username .Values.postgresql.password (include "leros.fullname" .) .Values.postgresql.database -}}
{{- else -}}
{{- required "postgresql.enabled=false 时必须设置 postgresql.external.url" .Values.postgresql.external.url -}}
{{- end -}}
{{- end -}}

{{/*
Build NATS URL. Uses builtin nats when enabled, otherwise external.url.
认证启用时在内置 URL 中携带 user:password。
*/}}
{{- define "leros.natsURL" -}}
{{- if .Values.nats.enabled -}}
{{- $host := printf "%s-nats:4222" (include "leros.fullname" .) -}}
{{- if .Values.nats.auth.enabled -}}
{{- printf "nats://%s:%s@%s" .Values.nats.auth.user .Values.nats.auth.password $host -}}
{{- else -}}
{{- printf "nats://%s" $host -}}
{{- end -}}
{{- else -}}
{{- required "nats.enabled=false 时必须设置 nats.external.url" .Values.nats.external.url -}}
{{- end -}}
{{- end -}}

{{/*
Put host paths under a shared host prefix.

Usage:
  {{ include "leros.hostPath" (dict "root" . "path" .Values.postgresql.hostPath) }}

The `path` value may contain the literal placeholder <dataHostPath>, which is
replaced with .Values.dataHostPath at render time. This lets values.yaml show
the structure (e.g. "<dataHostPath>/postgresql") while still allowing operators
to change the prefix in a single place.
*/}}
{{- define "leros.hostPath" -}}
{{- replace "<dataHostPath>" .root.Values.dataHostPath .path -}}
{{- end -}}

# --- 集群内部地址计算（无域名时默认用 Service 名）---

{{/*
Server baseUrl：留空自动用集群内部 Service 地址。
*/}}
{{- define "leros.serverBaseUrl" -}}
{{- if .Values.server.baseUrl }}{{ .Values.server.baseUrl }}{{- else }}{{ printf "http://%s:%s" (include "leros.serverServiceName" .) (.Values.server.service.port | toString) }}{{- end -}}
{{- end -}}
