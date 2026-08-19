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
Account 组件命名。经 .type 区分返回 service/deployment/configMap/secret 资源名。
统一走 nameOverride，默认 <fullname>-account。
用法：{{ include "leros.accountName" (dict "root" . "type" "service") }}
*/}}
{{- define "leros.accountName" -}}
{{- $prefix := printf "%s-account" (include "leros.fullname" .root) -}}
{{- if eq .type "service" -}}
{{- default $prefix .root.Values.account.serviceNameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if eq .type "deployment" -}}
{{- default $prefix .root.Values.account.deploymentNameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if eq .type "configMap" -}}
{{- default $prefix .root.Values.account.configMapNameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if eq .type "secret" -}}
{{- printf "%s-secret" $prefix | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $prefix -}}
{{- end -}}
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
Build MySQL URL. 优先级：
  1) account.reuseCorekg=true → 复用 corekg 命名空间的 MySQL（可配置 service 名/凭据）
  2) mysql.enabled → 内置 MySQL（account 独立部署场景）
  3) 否则用 mysql.external.url
内置 DSIs 带 mysql:// scheme（iam 要求的连接串格式）。
*/}}
{{- define "leros.mysqlURL" -}}
{{- if and .Values.account.enabled .Values.account.reuseCorekg -}}
{{- $cg := .Values.account.corekg | default dict -}}
{{- $db := default .Values.mysql.database (default "" $cg.mysqlDatabase) -}}
{{- $user := default .Values.mysql.username (default "" $cg.mysqlUsername) -}}
{{- $pass := default .Values.mysql.password (default "" $cg.mysqlPassword) -}}
{{- printf "mysql://%s:%s@%s.%s:3306/%s?charset=utf8mb4&parseTime=true&loc=Local&timeout=5s" $user $pass $cg.mysqlService (default "corekg" $cg.namespace) $db -}}
{{- else if .Values.mysql.enabled -}}
{{- printf "mysql://%s:%s@%s-mysql:3306/%s?charset=utf8mb4&parseTime=true&loc=Local&timeout=5s" .Values.mysql.username .Values.mysql.password (include "leros.fullname" .) .Values.mysql.database -}}
{{- else -}}
{{- required "mysql.enabled=false 时必须设置 mysql.external.url" .Values.mysql.external.url -}}
{{- end -}}
{{- end -}}

{{/*
Redis 地址。优先级：
  1) account.reuseCorekg=true → 复用 corekg 命名空间的 Redis
  2) redis.enabled → 内置 Redis（account 独立部署场景）
  3) 否则用 redis.external.url
*/}}
{{- define "leros.redisURL" -}}
{{- if and .Values.account.enabled .Values.account.reuseCorekg -}}
{{- $cg := .Values.account.corekg | default dict -}}
{{- $pass := default .Values.redis.password (default "" $cg.redisPassword) -}}
{{- printf "redis://:%s@%s.%s:6379/0" $pass $cg.redisService (default "corekg" $cg.namespace) -}}
{{- else if .Values.redis.enabled -}}
{{- printf "redis://:%s@%s-redis:6379/0" .Values.redis.password (include "leros.fullname" .) -}}
{{- else -}}
{{- required "redis.enabled=false 时必须设置 redis.external.url" .Values.redis.external.url -}}
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

{{/*
IngressClassName 自动计算：用户无需对齐 traefik.ingressClass.name 与 ingress.className。
优先级：用户显式设置 ingress.className > 自动跟随：
  - traefik.enabled=true  → 用 traefik.ingressClass.name（默认 leros-traefik）
  - traefik.enabled=false → traefik（复用 k3s 自带）
用户仍可显式覆盖（如 nginx 集群）。
*/}}
{{- define "leros.ingressClassName" -}}
{{- if .Values.ingress.className -}}{{- .Values.ingress.className -}}{{- else if .Values.traefik.enabled -}}{{- (get (.Values.traefik.ingressClass | default dict) "name") | default "leros-traefik" -}}{{- else -}}{{- "traefik" -}}{{- end -}}
{{- end -}}

{{/*
节点选择器回退：组件级 nodeSelector 优先，为空则用顶层 nodeSelector。
用法：{{ include "leros.nodeSelector" (dict "root" . "selector" .Values.postgresql.nodeSelector) }}
让用户只需在顶层 nodeSelector 填一处，postgresql/nats/server/worker 全部跟随。
*/}}
{{- define "leros.nodeSelector" -}}
{{- $sel := .selector | default dict -}}
{{- if $sel -}}{{- toYaml $sel -}}{{- else -}}{{- toYaml (.root.Values.nodeSelector | default dict) -}}{{- end -}}
{{- end -}}
