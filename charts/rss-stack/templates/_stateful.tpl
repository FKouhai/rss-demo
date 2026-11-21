{{/* Shared skeleton pieces for the Go services. */}}

{{/* Probes every Go service exposes. */}}
{{- define "rss-stack.probes" -}}
livenessProbe:
  httpGet: { path: /healthz, port: 3000 }
  initialDelaySeconds: 10
  periodSeconds: 10
readinessProbe:
  httpGet: { path: /ready, port: 3000 }
  initialDelaySeconds: 5
  periodSeconds: 5
{{- end -}}

{{/*
DB env for a stateful service. Call as
(dict "svc" <svcValues> "prefix" "OUTBOX_" "dir" "/var/lib/rss-outbox" "file" "outbox.db").
*/}}
{{- define "rss-stack.dbEnv" -}}
{{- $svc := .svc -}}
- name: {{ .prefix }}DB_DRIVER
  value: {{ $svc.db.driver | quote }}
{{- if eq $svc.db.driver "sqlite" }}
- name: {{ .prefix }}DB_DSN
  value: {{ printf "%s/%s" .dir .file | quote }}
{{- else }}
- name: {{ .prefix }}DB_DSN
  valueFrom:
    secretKeyRef:
      name: {{ required "postgres needs db.postgres.dsnSecret" $svc.db.postgres.dsnSecret | quote }}
      key: {{ $svc.db.postgres.dsnKey | default "dsn" | quote }}
{{- end -}}
{{- end -}}

{{/* Volume mount for sqlite (empty otherwise). Call with (dict "svc" ... "dir" ...). */}}
{{- define "rss-stack.dbVolumeMounts" -}}
{{- if eq .svc.db.driver "sqlite" }}
volumeMounts:
  - name: data
    mountPath: {{ .dir | quote }}
{{- end -}}
{{- end -}}

{{/* Pod volumes for sqlite (empty otherwise). Call with (dict "svc" ... "claim" ...). */}}
{{- define "rss-stack.dbVolumes" -}}
{{- if eq .svc.db.driver "sqlite" }}
volumes:
  - name: data
    persistentVolumeClaim:
      claimName: {{ .claim }}
{{- end -}}
{{- end -}}
