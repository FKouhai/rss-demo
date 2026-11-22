{{/* Expand the chart name, honouring an override. */}}
{{- define "rss-stack.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Release-scoped base name, DNS-safe. Standard Helm semantics: an
explicit fullnameOverride wins; otherwise a release named like the
chart (e.g. nixidy's rss-stack release of the rss-stack chart)
collapses to just the release name instead of "rss-stack-rss-stack".
*/}}
{{- define "rss-stack.fullname" -}}
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

{{/* Standard labels. */}}
{{- define "rss-stack.labels" -}}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: rss-stack
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Component selector labels. Call as (dict "root" $ "name" "poller"). */}}
{{- define "rss-stack.selector" -}}
app.kubernetes.io/name: {{ .name }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
{{- end -}}

{{/* Service image: global registry + per-service repo + tag (default appVersion). */}}
{{- define "rss-stack.image" -}}
{{- $tag := .tag | default .root.Chart.AppVersion -}}
{{- printf "%s/%s:%s" .root.Values.global.imageRegistry .repository $tag -}}
{{- end -}}

{{/* NATS namespace: shared-infra override or this release. */}}
{{- define "rss-stack.natsNamespace" -}}
{{- .Values.nats.namespace | default .Release.Namespace -}}
{{- end -}}

{{/*
NATS URL for a service: per-service override wins; otherwise the
embedded service (namespace-aware); empty when NATS is disabled and no
override is given (services fail closed on connect).
*/}}
{{- define "rss-stack.natsUrl" -}}
{{- if .svc.nats.url -}}
{{- .svc.nats.url -}}
{{- else if .root.Values.nats.enabled -}}
{{- printf "nats://%s-nats.%s.svc.cluster.local:4222" (include "rss-stack.fullname" .root) (include "rss-stack.natsNamespace" .root) -}}
{{- end -}}
{{- end -}}

{{/* True when any NATS authSecret is set (warn: not yet consumed). */}}
{{- define "rss-stack.anyNatsAuth" -}}
{{- $auth := list .Values.services.poller.nats.authSecret .Values.services.outbox.nats.authSecret .Values.services.notify.nats.authSecret .Values.services.aggregator.nats.authSecret -}}
{{- $set := list -}}
{{- range $auth }}{{ if . }}{{ $set = append $set . }}{{ end }}{{ end -}}
{{- if $set }}true{{- end -}}
{{- end -}}

{{/* In-chart service DNS base. Call as (dict "root" $ "svc" "config"). */}}
{{- define "rss-stack.svcUrl" -}}
{{- printf "http://%s-%s:3000" (include "rss-stack.fullname" .root) .svc -}}
{{- end -}}
