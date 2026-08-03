{{/*
强制校验：启用本 chart 的 Traefik 时避免与 k3s 自带实例冲突。
规则（traefik.enabled=true 时全部生效，违反任一即 fail 渲染）：
  1) traefik.ingressClass.name 不得为 "traefik"（与 k3s 默认类名撞）
  2) traefik.ports.web/websecure.hostPort 必须显式设置且不等于 80/443
未启用本 chart Traefik（traefik.enabled=false）时不做任何校验，复用 k3s 自带实例即可。
ingress.className 无需用户对齐，由 leros.ingressClassName 自动跟随 traefik.ingressClass.name。

用法：在任一顶层模板首行调用
  {{- include "leros.validateTraefik" . -}}
*/}}
{{- define "leros.validateTraefik" -}}
{{- if .Values.traefik.enabled -}}
{{- $ic := .Values.traefik.ingressClass | default dict -}}
{{- $icName := $ic.name | default "" -}}
{{- if eq $icName "traefik" -}}
{{- fail "traefik.enabled=true 时 traefik.ingressClass.name 不得为 \"traefik\"（与 k3s 自带 Traefik 的 IngressClass 冲突）。请改为不同名（如 leros-traefik）。" -}}
{{- end -}}
{{- if eq $icName "" -}}
{{- fail "traefik.enabled=true 时必须显式设置 traefik.ingressClass.name（不得为空，且不能等于 traefik）。" -}}
{{- end -}}
{{- $ports := .Values.traefik.ports | default dict -}}
{{- $webHP := (get ($ports.web | default dict) "hostPort") -}}
{{- $webSecHP := (get ($ports.websecure | default dict) "hostPort") -}}
{{- if or (empty $webHP) (empty $webSecHP) -}}
{{- fail "traefik.enabled=true 时必须显式设置 traefik.ports.web.hostPort 与 traefik.ports.websecure.hostPort（且不得为 80/443），以避免与 k3s 自带 Traefik 端口冲突。" -}}
{{- end -}}
{{- $webHPn := int (toString $webHP) -}}
{{- $webSecHPn := int (toString $webSecHP) -}}
{{- if or (eq $webHPn 80) (eq $webHPn 443) (eq $webSecHPn 80) (eq $webSecHPn 443) -}}
{{- fail "traefik.enabled=true 时 traefik.ports.web.hostPort 与 traefik.ports.websecure.hostPort 不得为 80/443（k3s 自带 Traefik 已占用）。请改为其他端口（如 8080/8443）。" -}}
{{- end -}}
{{- end -}}
{{- end -}}
