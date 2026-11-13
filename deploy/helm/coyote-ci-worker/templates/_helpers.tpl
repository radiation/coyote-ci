{{- define "coyote-ci-worker.validate" -}}
{{- $namespace := include "coyote-ci-worker.namespace" . -}}
{{- if ne .Release.Namespace $namespace }}{{ fail "release namespace must match namespace" }}{{ end -}}
{{- if and (eq $namespace .Values.controlPlaneNamespace) (not .Values.allowSharedControlPlaneNamespace) }}{{ fail "control-plane and execution namespaces must differ unless allowSharedControlPlaneNamespace is true" }}{{ end -}}
{{- $csi := and .Values.gke.enabled .Values.gke.secretManagerCSI.enabled -}}
{{- $identity := and .Values.gke.enabled .Values.gke.workloadIdentity.enabled -}}
{{- if and (not .Values.gke.enabled) (or .Values.gke.secretManagerCSI.enabled .Values.gke.workloadIdentity.enabled) }}{{ fail "GKE integrations require explicit gke.enabled=true" }}{{ end -}}
{{- if .Values.cloudSql.enabled -}}
{{- if not $identity }}{{ fail "cloudSql.enabled requires GKE workload identity" }}{{ end -}}
{{- $_ := required "cloudSql.connectionName is required" .Values.cloudSql.connectionName -}}
{{- else if .Values.cloudSql.connectionName }}{{ fail "cloudSql.connectionName requires explicit cloudSql.enabled=true; update existing GKE values" }}{{ end -}}
{{- if $identity -}}
{{- $_ := required "serviceAccounts.worker.gsaEmail is required for workload identity" .Values.serviceAccounts.worker.gsaEmail -}}
{{- else if .Values.serviceAccounts.worker.gsaEmail }}{{ fail "gsaEmail requires explicit GKE workload identity opt-ins; update existing GKE values" }}{{ end -}}
{{- if $csi -}}
{{- $_ := required "secrets.project is required for CSI" .Values.secrets.project -}}
{{- $_ := required "secrets.databaseUrl is required for CSI" .Values.secrets.databaseUrl -}}
{{- if or .Values.secrets.existingSecrets.databaseUrl.name .Values.secrets.existingSecrets.databaseUrl.key }}{{ fail "choose CSI or native Secrets, not both" }}{{ end -}}
{{- else -}}
{{- if or .Values.secrets.project .Values.secrets.databaseUrl }}{{ fail "legacy Secret Manager references require explicit GKE CSI opt-ins; update existing GKE values" }}{{ end -}}
{{- include "coyote-ci-worker.requireSecret" (dict "name" "databaseUrl" "reference" .Values.secrets.existingSecrets.databaseUrl) -}}
{{- end -}}
{{- range $name, $reference := .Values.worker.extraEnvSecrets -}}
{{- if regexMatch "^(DATABASE_|DB_|ARTIFACT_|WORKSPACE_|WORKER_|COYOTE_|AUTH_|OIDC_|SESSION_|CLOUD_BUILD_|GOOGLE_CLOUD_PROJECT$)" $name }}{{ fail "worker.extraEnvSecrets must not override chart-managed configuration" }}{{ end -}}
{{- include "coyote-ci-worker.requireSecret" (dict "name" $name "reference" $reference) -}}
{{- end -}}
{{- if eq .Values.storage.provider "filesystem" -}}
{{- if or .Values.storage.project .Values.storage.artifacts.bucket }}{{ fail "GCS settings require explicit storage.provider=gcs; update existing GKE values" }}{{ end -}}
{{- if .Values.cloudBuild.enabled }}{{ fail "Cloud Build requires GCS artifact storage; filesystem artifacts are owned by the server" }}{{ end -}}
{{- else -}}
{{- $_ := required "storage.artifacts.bucket is required for GCS" .Values.storage.artifacts.bucket -}}
{{- end -}}
{{- if .Values.cloudBuild.enabled -}}
{{- range $key := list "project" "location" "runtimeServiceAccount" "artifactRegistryRepository" -}}
{{- $_ := required (printf "cloudBuild.%s is required" $key) (index $.Values.cloudBuild $key) -}}
{{- end -}}
{{- $_ := required "cloudBuild.source.bucket is required" .Values.cloudBuild.source.bucket -}}
{{- else if or .Values.cloudBuild.project .Values.cloudBuild.location .Values.cloudBuild.runtimeServiceAccount .Values.cloudBuild.artifactRegistryRepository .Values.cloudBuild.source.bucket }}{{ fail "Cloud Build settings require explicit cloudBuild.enabled=true; update existing GKE values" }}{{ end -}}
{{- end -}}

{{- define "coyote-ci-worker.namespace" -}}
{{- default .Release.Namespace .Values.namespace -}}
{{- end -}}

{{- define "coyote-ci-worker.requireSecret" -}}
{{- if or (not .reference.name) (not .reference.key) }}{{ fail (printf "Secret reference %s requires name and key" .name) }}{{ end -}}
{{- end -}}

{{- define "coyote-ci-worker.annotations" -}}
{{- $annotations := deepCopy .account.annotations -}}
{{- if and .root.Values.gke.enabled .root.Values.gke.workloadIdentity.enabled .account.gsaEmail -}}
{{- $_ := set $annotations "iam.gke.io/gcp-service-account" .account.gsaEmail -}}
{{- else if hasKey $annotations "iam.gke.io/gcp-service-account" }}{{ fail "Google service-account annotations require workload identity opt-ins" }}{{ end -}}
{{- with $annotations }}
annotations:
  {{- toYaml . | nindent 2 }}
{{- end -}}
{{- end -}}

{{- define "coyote-ci-worker.databaseVolume" -}}
- name: database-url
  {{- if and .Values.gke.enabled .Values.gke.secretManagerCSI.enabled }}
  csi:
    driver: secrets-store-gke.csi.k8s.io
    readOnly: true
    volumeAttributes: {secretProviderClass: {{ .Values.worker.databaseSecretProviderClass }}}
  {{- else }}
  secret:
    secretName: {{ .Values.secrets.existingSecrets.databaseUrl.name | quote }}
    defaultMode: 0444
    items:
      - key: {{ .Values.secrets.existingSecrets.databaseUrl.key | quote }}
        path: database-url
  {{- end }}
{{- end -}}

{{- define "coyote-ci-worker.artifactEnv" -}}
- {name: ARTIFACT_STORAGE_PROVIDER, value: {{ .Values.storage.provider }}}
{{- if eq .Values.storage.provider "gcs" }}
{{- if not .Values.worker.legacyEnvironmentLayout }}
- {name: ARTIFACT_STORAGE_STRICT, value: {{ .Values.storage.artifacts.strict | quote }}}
{{- end }}
- {name: ARTIFACT_GCS_BUCKET, value: {{ .Values.storage.artifacts.bucket | quote }}}
- {name: ARTIFACT_GCS_PREFIX, value: {{ .Values.storage.artifacts.prefix | quote }}}
- {name: ARTIFACT_GCS_PROJECT, value: {{ .Values.storage.project | quote }}}
{{- if .Values.worker.legacyEnvironmentLayout }}
- {name: ARTIFACT_STORAGE_STRICT, value: {{ .Values.storage.artifacts.strict | quote }}}
{{- end }}
{{- end }}
{{- end -}}
