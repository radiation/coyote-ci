{{- define "coyote-ci-worker.validate" -}}
{{- if ne .Release.Namespace .Values.namespace }}{{ fail "release namespace must match namespace" }}{{ end -}}
{{- if eq .Values.namespace .Values.controlPlaneNamespace }}{{ fail "control-plane and execution namespaces must differ" }}{{ end -}}
{{- end -}}
