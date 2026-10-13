{{- define "coyote-ci.validate" -}}
{{- if ne .Release.Namespace .Values.namespace }}{{ fail "release namespace must match namespace" }}{{ end -}}
{{- if eq .Values.namespace .Values.executionNamespace }}{{ fail "control-plane and execution namespaces must differ" }}{{ end -}}
{{- if ne .Values.auth.oidc.redirectURL (printf "%s/auth/callback" (trimSuffix "/" .Values.auth.publicURL)) }}{{ fail "auth.oidc.redirectURL must equal auth.publicURL/auth/callback" }}{{ end -}}
{{- if and .Values.migration.render (eq .Values.migration.runID "") }}{{ fail "migration.runID is required when migration.render is true" }}{{ end -}}
{{- if .Values.mcp.enabled -}}
{{- if eq .Values.auth.mode "disabled" }}{{ fail "auth.mode must not be disabled when mcp.enabled is true" }}{{ end -}}
{{- if eq .Values.images.mcp "" }}{{ fail "images.mcp is required when mcp.enabled is true" }}{{ end -}}
{{- if eq .Values.serviceAccounts.mcp.gsaEmail "" }}{{ fail "serviceAccounts.mcp.gsaEmail is required when mcp.enabled is true" }}{{ end -}}
{{- if .Values.mcp.auth.allowDisabledAuth }}{{ fail "mcp.auth.allowDisabledAuth must remain false" }}{{ end -}}
{{- end -}}
{{- end -}}
