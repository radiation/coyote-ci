{{- define "coyote-ci.validate" -}}
{{- if ne .Release.Namespace .Values.namespace }}{{ fail "release namespace must match namespace" }}{{ end -}}
{{- if eq .Values.namespace .Values.executionNamespace }}{{ fail "control-plane and execution namespaces must differ" }}{{ end -}}
{{- if ne .Values.auth.oidc.redirectURL (printf "%s/auth/callback" (trimSuffix "/" .Values.auth.publicURL)) }}{{ fail "auth.oidc.redirectURL must equal auth.publicURL/auth/callback" }}{{ end -}}
{{- if and .Values.migration.render (eq .Values.migration.runID "") }}{{ fail "migration.runID is required when migration.render is true" }}{{ end -}}
{{- end -}}
