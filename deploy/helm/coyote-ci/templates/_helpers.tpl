{{- define "coyote-ci.tokenReviewName" -}}
{{- if .Values.rbac.tokenReviewNameOverride -}}
{{- .Values.rbac.tokenReviewNameOverride -}}
{{- else -}}
{{- $identity := printf "%s-%s" .Release.Name .Release.Namespace -}}
{{- $prefix := trunc 40 $identity | trimSuffix "-" -}}
{{- printf "%s-%s-tokenreview" $prefix (trunc 8 (sha256sum $identity)) -}}
{{- end -}}
{{- end -}}

{{- define "coyote-ci.validate" -}}
{{- $namespace := include "coyote-ci.namespace" . -}}
{{- if ne .Release.Namespace $namespace }}{{ fail "release namespace must match namespace" }}{{ end -}}
{{- if and (eq $namespace .Values.executionNamespace) (not .Values.allowSharedExecutionNamespace) }}{{ fail "control-plane and execution namespaces must differ unless allowSharedExecutionNamespace is true" }}{{ end -}}
{{- if ne .Values.auth.oidc.redirectURL (printf "%s/auth/callback" (trimSuffix "/" .Values.auth.publicURL)) }}{{ fail "auth.oidc.redirectURL must equal auth.publicURL/auth/callback" }}{{ end -}}
{{- $_ := required "compatibility.schema_target is required; obtain it from the pinned migration image or V3 manifest" .Values.compatibility.schema_target -}}
{{- if or (hasKey .Values.migration "render") (hasKey .Values.migration "runID") }}{{ fail "manual migration.render/runID controls are no longer supported" }}{{ end -}}
{{- $source := include "coyote-ci.migrationSource" . -}}
{{- if and $source .Values.release.version -}}
{{- if semverCompare (printf "<%s" $source) .Values.release.version }}{{ fail "application downgrades are unsupported; Helm rollback does not reverse database migrations" }}{{ end -}}
{{- if and (ne $source .Values.release.version) (not .Values.migration.maintenanceAcknowledged) -}}
{{- if or (not .Values.compatibility.upgrade_from) (not (semverCompare .Values.compatibility.upgrade_from $source)) }}{{ fail "source release is outside compatibility.upgrade_from; a separately approved maintenance window is required" }}{{ end -}}
{{- end -}}
{{- end -}}
{{- $jobs := lookup "batch/v1" "Job" $namespace "" -}}
{{- range $jobs.items -}}
{{- if and (eq (dig "app.kubernetes.io/name" "" (.metadata.labels | default dict)) "coyote-migrate") (eq (dig "app.kubernetes.io/instance" "" (.metadata.labels | default dict)) $.Release.Name) -}}
{{- $terminal := false -}}
{{- range (.status.conditions | default list) -}}
{{- if and (eq .status "True") (or (eq .type "Complete") (eq .type "Failed")) }}{{ $terminal = true }}{{ end -}}
{{- end -}}
{{- if not $terminal }}{{ fail "an earlier migration Job is not terminal; inspect it before retrying" }}{{ end -}}
{{- end -}}
{{- end -}}
{{- if .Values.mcp.enabled -}}
{{- if eq .Values.auth.mode "disabled" }}{{ fail "auth.mode must not be disabled when mcp.enabled is true" }}{{ end -}}
{{- if eq .Values.images.mcp "" }}{{ fail "images.mcp is required when mcp.enabled is true" }}{{ end -}}
{{- if .Values.mcp.auth.allowDisabledAuth }}{{ fail "mcp.auth.allowDisabledAuth must remain false" }}{{ end -}}
{{- end -}}
{{- $csi := and .Values.gke.enabled .Values.gke.secretManagerCSI.enabled -}}
{{- $identity := and .Values.gke.enabled .Values.gke.workloadIdentity.enabled -}}
{{- if and (not .Values.gke.enabled) (or .Values.gke.secretManagerCSI.enabled .Values.gke.workloadIdentity.enabled .Values.gateway.enabled) }}{{ fail "GKE integrations require explicit gke.enabled=true" }}{{ end -}}
{{- if and .Values.cloudSql.enabled (not $identity) }}{{ fail "cloudSql.enabled requires GKE workload identity" }}{{ end -}}
{{- if .Values.cloudSql.enabled -}}
{{- if semverCompare "<1.29.0-0" .Capabilities.KubeVersion.Version }}{{ fail "Cloud SQL migration sidecars require Kubernetes 1.29+ with native sidecars enabled" }}{{ end -}}
{{- $_ := required "cloudSql.connectionName is required" .Values.cloudSql.connectionName -}}
{{- else if .Values.cloudSql.connectionName }}{{ fail "cloudSql.connectionName requires explicit cloudSql.enabled=true; update existing GKE values" }}{{ end -}}
{{- range $name := list "server" "migrate" "mcp" -}}
{{- $account := index $.Values.serviceAccounts $name -}}
{{- if and $identity (or (ne $name "mcp") $.Values.mcp.enabled) -}}
{{- $_ := required (printf "serviceAccounts.%s.gsaEmail is required for workload identity" $name) $account.gsaEmail -}}
{{- else if and (not $identity) $account.gsaEmail }}{{ fail "gsaEmail requires explicit GKE workload identity opt-ins; update existing GKE values" }}{{ end -}}
{{- end -}}
{{- if $csi -}}
{{- $_ := required "secrets.project is required for CSI" .Values.secrets.project -}}
{{- range $name := list "databaseUrl" "workspaceHelperCapability" "oidcClient" "session" -}}
{{- $_ := required (printf "secrets.%s is required for CSI" $name) (index $.Values.secrets $name) -}}
{{- $reference := index $.Values.secrets.existingSecrets $name -}}
{{- if or $reference.name $reference.key }}{{ fail "choose CSI or native Secrets, not both" }}{{ end -}}
{{- end -}}
{{- else -}}
{{- range $name := list "project" "databaseUrl" "workspaceHelperCapability" "oidcClient" "session" -}}
{{- if index $.Values.secrets $name }}{{ fail "legacy Secret Manager references require explicit GKE CSI opt-ins; update existing GKE values" }}{{ end -}}
{{- end -}}
{{- range $name, $reference := .Values.secrets.existingSecrets -}}
{{- include "coyote-ci.requireSecret" (dict "name" $name "reference" $reference) -}}
{{- end -}}
{{- end -}}
{{- range $name, $reference := .Values.server.extraEnvSecrets -}}
{{- if regexMatch "^(DATABASE_|DB_|ARTIFACT_|WORKSPACE_|WORKER_|COYOTE_|AUTH_|OIDC_|SESSION_|BOOTSTRAP_|CLOUD_BUILD_|GOOGLE_CLOUD_PROJECT$)" $name }}{{ fail "server.extraEnvSecrets must not override chart-managed configuration" }}{{ end -}}
{{- include "coyote-ci.requireSecret" (dict "name" $name "reference" $reference) -}}
{{- end -}}
{{- if eq .Values.storage.provider "filesystem" -}}
{{- if or (gt (int .Values.server.replicas) 1) .Values.server.replicasManagedExternally }}{{ fail "filesystem storage requires at most one Helm-managed server replica; HA is unsupported" }}{{ end -}}
{{- if or .Values.storage.project .Values.storage.artifacts.bucket .Values.storage.cache.bucket .Values.storage.workspaceRevisions.bucket }}{{ fail "GCS settings require explicit storage.provider=gcs; update existing GKE values" }}{{ end -}}
{{- else -}}
{{- range $name := list "artifacts" "cache" "workspaceRevisions" -}}
{{- $_ := required (printf "storage.%s.bucket is required for GCS" $name) (index $.Values.storage $name).bucket -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "coyote-ci.namespace" -}}
{{- default .Release.Namespace .Values.namespace -}}
{{- end -}}

{{- define "coyote-ci.migrationSource" -}}
{{- $previous := lookup "v1" "ConfigMap" (include "coyote-ci.namespace" .) "coyote-control-plane" -}}
{{- $recorded := dig "metadata" "annotations" "coyote-ci/release-version" "" ($previous | default dict) -}}
{{- $digest := dig "metadata" "annotations" "coyote-ci/manifest-digest" "" ($previous | default dict) -}}
{{- if and $recorded (eq $recorded .Values.release.version) $digest .Values.release.manifestDigest (ne $digest .Values.release.manifestDigest) }}{{ fail "an application release version cannot change manifest digest" }}{{ end -}}
{{- if and $recorded .Values.migration.sourceVersion (ne $recorded .Values.migration.sourceVersion) }}{{ fail "migration.sourceVersion conflicts with recorded application version" }}{{ end -}}
{{- default .Values.migration.sourceVersion $recorded -}}
{{- end -}}

{{- define "coyote-ci.onlineMigrationApproved" -}}
{{- $source := include "coyote-ci.migrationSource" . -}}
{{- if and .Values.migration.onlineUpgradeApproved $source .Values.release.version .Values.compatibility.upgrade_from -}}
{{- semverCompare .Values.compatibility.upgrade_from $source -}}
{{- else -}}false{{- end -}}
{{- end -}}

{{- define "coyote-ci.requireSecret" -}}
{{- if or (not .reference.name) (not .reference.key) }}{{ fail (printf "Secret reference %s requires name and key" .name) }}{{ end -}}
{{- end -}}

{{- define "coyote-ci.annotations" -}}
{{- $annotations := deepCopy .account.annotations -}}
{{- if and .root.Values.gke.enabled .root.Values.gke.workloadIdentity.enabled -}}
{{- $_ := set $annotations "iam.gke.io/gcp-service-account" .account.gsaEmail -}}
{{- else if hasKey $annotations "iam.gke.io/gcp-service-account" }}{{ fail "Google service-account annotations require workload identity opt-ins" }}{{ end -}}
{{- with $annotations }}
annotations:
  {{- toYaml . | nindent 2 }}
{{- end -}}
{{- end -}}

{{- define "coyote-ci.secretVolume" -}}
- name: {{ .name }}
  {{- if and .root.Values.gke.enabled .root.Values.gke.secretManagerCSI.enabled }}
  csi:
    driver: secrets-store-gke.csi.k8s.io
    readOnly: true
    volumeAttributes: {secretProviderClass: {{ .providerClass }}}
  {{- else }}
  projected:
    defaultMode: 0444
    sources:
      {{- $keys := dict "databaseUrl" "database-url" -}}
      {{- if not .databaseOnly -}}
      {{- $_ := set $keys "workspaceHelperCapability" "workspace-helper-capability-secret" -}}
      {{- $_ := set $keys "oidcClient" "oidc-client-secret" -}}
      {{- $_ := set $keys "session" "session-secret" -}}
      {{- end -}}
      {{- range $key, $path := $keys }}
      - secret:
          name: {{ (index $.root.Values.secrets.existingSecrets $key).name | quote }}
          items:
            - key: {{ (index $.root.Values.secrets.existingSecrets $key).key | quote }}
              path: {{ $path }}
      {{- end }}
  {{- end }}
{{- end -}}

{{- define "coyote-ci.claimName" -}}
{{- default "coyote-data" .Values.storage.filesystem.existingClaim -}}
{{- end -}}
