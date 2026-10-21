{{- define "coyote-ci-worker.validate" -}}
{{- if ne .Release.Namespace .Values.namespace }}{{ fail "release namespace must match namespace" }}{{ end -}}
{{- if and (eq .Values.namespace .Values.controlPlaneNamespace) (not .Values.allowSharedControlPlaneNamespace) }}{{ fail "control-plane and execution namespaces must differ unless allowSharedControlPlaneNamespace is true" }}{{ end -}}
{{- end -}}
