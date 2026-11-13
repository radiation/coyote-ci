#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$temp_dir"
}
trap cleanup EXIT

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

expect_template_failure() {
  local name="$1"
  local chart="$2"
  local namespace="$3"
  local values="$4"
  local override="$5"

  if helm template "$name" "$chart" --namespace "$namespace" --values "$values" --values "$override" >/dev/null 2>&1; then
    echo "expected Helm template validation failure for $name" >&2
    exit 1
  fi
}

require_command helm

control_values="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml"
worker_values="$repo_root/deploy/helm/examples/gke-staging-worker-values.yaml"
production_control_values="$repo_root/deploy/helm/examples/gke-production-control-plane-values.yaml"
production_worker_values="$repo_root/deploy/helm/examples/gke-production-worker-values.yaml"

helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$production_control_values" >/dev/null
helm template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace coyote-ci --values "$production_worker_values" >"$temp_dir/production-worker.yaml"
ruby -ryaml - "$temp_dir/production-worker.yaml" <<'RUBY'
documents = YAML.load_stream(File.read(ARGV.fetch(0))).compact
names = documents.map { |document| document.dig("metadata", "name") if ["Role", "RoleBinding"].include?(document["kind"]) }.compact
abort "shared-namespace worker must not render duplicate verifier RBAC" if names.include?("coyote-server-workspace-verifier")
worker = documents.find { |document| document.dig("metadata", "name") == "coyote-kubernetes-worker" && document["kind"] == "Deployment" } or abort "production worker deployment is missing"
helper_image = worker.dig("spec", "template", "spec", "containers")&.find { |container| container["name"] == "worker" }&.fetch("env")&.find { |env| env["name"] == "WORKER_KUBERNETES_HELPER_IMAGE" }&.fetch("value")
abort "production helper image must use the worker image by default" unless helper_image == worker.dig("spec", "template", "spec", "containers")&.find { |container| container["name"] == "worker" }&.fetch("image")
RUBY

cat >"$temp_dir/shared-namespace-without-opt-in.yaml" <<'EOF'
executionNamespace: coyote-ci
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/shared-namespace-without-opt-in.yaml"

cat >"$temp_dir/shared-worker-namespace-without-opt-in.yaml" <<'EOF'
namespace: coyote-ci
controlPlaneNamespace: coyote-ci
EOF
expect_template_failure coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" coyote-ci "$worker_values" "$temp_dir/shared-worker-namespace-without-opt-in.yaml"

cat >"$temp_dir/missing-namespace.yaml" <<'EOF'
namespace: mismatched-namespace
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/missing-namespace.yaml"

cat >"$temp_dir/invalid-image.yaml" <<'EOF'
images:
  server: example.invalid/coyote-server:mutable
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/invalid-image.yaml"

cat >"$temp_dir/incomplete-oidc.yaml" <<'EOF'
auth:
  oidc:
    issuerURL: ""
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/incomplete-oidc.yaml"

cat >"$temp_dir/incomplete-gateway.yaml" <<'EOF'
gateway:
  enabled: true
  addressName: ""
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/incomplete-gateway.yaml"

cat >"$temp_dir/invalid-mcp-image.yaml" <<'EOF'
mcp:
  enabled: true
images:
  mcp: example.invalid/coyote-mcp:mutable
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/invalid-mcp-image.yaml"

cat >"$temp_dir/invalid-mcp-auth.yaml" <<'EOF'
mcp:
  enabled: true
  auth:
    allowDisabledAuth: true
images:
  mcp: example.invalid/coyote-mcp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/invalid-mcp-auth.yaml"

cat >"$temp_dir/disabled-auth-mcp.yaml" <<'EOF'
auth:
  mode: disabled
mcp:
  enabled: true
images:
  mcp: example.invalid/coyote-mcp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
expect_template_failure coyote-ci "$repo_root/deploy/helm/coyote-ci" coyote-ci "$control_values" "$temp_dir/disabled-auth-mcp.yaml"

cat >"$temp_dir/mcp-enabled.yaml" <<'EOF'
mcp:
  enabled: true
  replicas: 2
  auth:
    allowedOrigins: ["https://mcp.example.invalid"]
images:
  mcp: example.invalid/coyote-mcp@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
serviceAccounts:
  mcp:
    name: coyote-mcp
    gsaEmail: coyote-gke-mcp@example-project.iam.gserviceaccount.com
EOF
helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$control_values" --values "$temp_dir/mcp-enabled.yaml" >"$temp_dir/mcp-enabled-render.yaml"
ruby -ryaml - "$temp_dir/mcp-enabled-render.yaml" <<'RUBY'
documents = YAML.load_stream(File.read(ARGV.fetch(0))).compact
index = documents.each_with_object({}) { |document, result| result[[document.fetch("kind"), document.dig("metadata", "name")]] = document }
deployment = index.fetch(["Deployment", "coyote-mcp"])
service = index.fetch(["Service", "coyote-mcp"])
account = index.fetch(["ServiceAccount", "coyote-mcp"])
secrets = index.fetch(["SecretProviderClass", "coyote-mcp-secrets"])
abort "MCP replicas did not render" unless deployment.dig("spec", "replicas") == 2
abort "MCP image is not digest-pinned" unless deployment.dig("spec", "template", "spec", "containers", 0, "image")&.include?("@sha256:")
abort "MCP service must be ClusterIP without session affinity" unless service.dig("spec", "type") == "ClusterIP" && service.dig("spec", "sessionAffinity") == "None"
abort "MCP probes are missing" unless deployment.dig("spec", "template", "spec", "containers", 0, "livenessProbe", "httpGet", "path") == "/healthz" && deployment.dig("spec", "template", "spec", "containers", 0, "readinessProbe", "httpGet", "path") == "/readyz"
abort "MCP pod must mount its service-account token for Workload Identity" unless deployment.dig("spec", "template", "spec", "automountServiceAccountToken") == true
abort "MCP security context is incomplete" unless deployment.dig("spec", "template", "spec", "containers", 0, "securityContext", "readOnlyRootFilesystem") == true
abort "MCP service account was not rendered" unless account.dig("metadata", "annotations", "iam.gke.io/gcp-service-account")
abort "MCP secret provider must only mount the database URL" unless YAML.load(secrets.dig("spec", "parameters", "secrets")).map { |item| item["path"] } == ["database-url"]
abort "MCP must not render Kubernetes RBAC" if documents.any? { |document| ["Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding"].include?(document["kind"]) && document.dig("metadata", "name")&.include?("mcp") }
RUBY

cat >"$temp_dir/invalid-concurrency.yaml" <<'EOF'
worker:
  maxInFlightJobs: 0
EOF
expect_template_failure coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" coyote-ci-staging "$worker_values" "$temp_dir/invalid-concurrency.yaml"

cat >"$temp_dir/mutable-helper-image.yaml" <<'EOF'
worker:
  helperImage: example.invalid/coyote-worker:mutable
EOF
helm template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace coyote-ci-staging --values "$worker_values" --values "$temp_dir/mutable-helper-image.yaml" >/dev/null

cat >"$temp_dir/release-overlay.yaml" <<'EOF'
release:
  version: 2.5.1
  channel: stable
  manifestDigest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
images:
  server: example.invalid/coyote-server@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  frontend: example.invalid/coyote-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
  worker: example.invalid/coyote-worker@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
  migrate: example.invalid/coyote-migrate@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
EOF
helm template coyote-ci "$repo_root/deploy/helm/coyote-ci" --namespace coyote-ci --values "$control_values" --values "$temp_dir/release-overlay.yaml" >/dev/null
helm template coyote-ci-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace coyote-ci-staging --values "$worker_values" --values "$temp_dir/release-overlay.yaml" >/dev/null

ruby -ryaml -rjson -ropen3 - "$repo_root" <<'RUBY'
root = ARGV.fetch(0)

def render(root, worker: false, overrides: {}, failure: nil, gke: false)
  chart = worker ? "coyote-ci-worker" : "coyote-ci"
  namespace = worker ? "portable-execution" : "portable-control"
  example = worker ? "worker" : "control-plane"
  if gke
    namespace = worker ? "coyote-ci-staging" : "coyote-ci"
    example = "gke-staging-#{example}"
  else
    example = "generic-#{example}"
  end
  output, error, result = Open3.capture3("helm", "template", "portable", "#{root}/deploy/helm/#{chart}",
    "--namespace", namespace, "--values", "#{root}/deploy/helm/examples/#{example}-values.yaml",
    "--values", "-", stdin_data: JSON.generate(overrides))
  if failure
    abort "expected failure containing #{failure.inspect}: #{error}" if result.success? || !error.include?(failure)
    return
  end
  abort error unless result.success?
  if !gke && output.match?(/secrets-store|cloud-sql|iam\.gke|GOOGLE_|CLOUD_BUILD_|_GCS_|SecretProviderClass|networking\.gke/)
    abort "generic render contains cloud integration"
  end
  documents = YAML.load_stream(output).compact
  abort "credential values must not be rendered" if documents.any? { |doc| doc["kind"] == "Secret" }
  documents.each do |document|
    next if document["kind"].start_with?("Cluster")
    abort "incorrect namespace" unless document.dig("metadata", "namespace") == namespace
  end
  documents
end

def resource(documents, kind, name)
  documents.find { |doc| doc["kind"] == kind && doc.dig("metadata", "name") == name } or abort "missing #{kind}/#{name}"
end

control = render(root)
migration_phase = render(root, overrides: {"server" => {"replicas" => 0}, "frontend" => {"replicas" => 0}, "migration" => {"render" => true, "runID" => "portable"}})
abort "zero-replica migration must not wait on an unconsumed PVC" if migration_phase.any? { |doc| doc["kind"] == "PersistentVolumeClaim" }
resource(migration_phase, "Job", "coyote-migrate-portable")
deployment = resource(control, "Deployment", "coyote-server")
pod = deployment.dig("spec", "template", "spec")
abort "filesystem server requires Recreate" unless deployment.dig("spec", "strategy", "type") == "Recreate"
abort "filesystem ownership must be non-root" unless pod.dig("securityContext", "runAsNonRoot") && pod.dig("securityContext", "runAsUser") == 10001 && pod.dig("securityContext", "fsGroup") == 10001
mount = pod.fetch("containers").first.fetch("volumeMounts").find { |entry| entry["name"] == "data" }
abort "durable mount is missing" unless mount["mountPath"] == "/var/lib/coyote"
volume = pod.fetch("volumes").find { |entry| entry["name"] == "data" }
claim = resource(control, "PersistentVolumeClaim", volume.dig("persistentVolumeClaim", "claimName"))
abort "PVC must be retained" unless claim.dig("metadata", "annotations", "helm.sh/resource-policy") == "keep"
abort "PVC access mode incorrect" unless claim.dig("spec", "accessModes") == ["ReadWriteOnce"]
abort "default storage class must be omitted" if claim.fetch("spec").key?("storageClassName")
config = resource(control, "ConfigMap", "coyote-control-plane").fetch("data")
%w[ARTIFACT_STORAGE_ROOT WORKER_CACHE_STORAGE_ROOT COYOTE_WORKSPACE_REVISION_STORAGE_ROOT].each do |key|
  abort "#{key} is not persistent" unless config.fetch(key).start_with?(mount["mountPath"] + "/")
end
references = pod.fetch("volumes").find { |entry| entry["name"] == "control-plane-secrets" }.dig("projected", "sources")
abort "server must project four selected keys" unless references.length == 4 && references.all? { |entry| entry.dig("secret", "items").length == 1 }
database = references.find { |entry| entry.dig("secret", "name") == "coyote-database" }
abort "database Secret selection incorrect" unless database.dig("secret", "items") == [{"key" => "url", "path" => "database-url"}]

extras = render(root, overrides: {"serviceAccounts" => {"server" => {"annotations" => {"example.org/identity" => "server"}}}, "server" => {"extraEnvSecrets" => {"GITHUB_STATUS_TOKEN" => {"name" => "github", "key" => "token"}}}})
abort "custom annotations missing" unless resource(extras, "ServiceAccount", "coyote-server").dig("metadata", "annotations", "example.org/identity") == "server"
env = resource(extras, "Deployment", "coyote-server").dig("spec", "template", "spec", "containers").first.fetch("env")
abort "extra credential must use secretKeyRef" unless env.find { |entry| entry["name"] == "GITHUB_STATUS_TOKEN" }&.dig("valueFrom", "secretKeyRef") == {"name" => "github", "key" => "token"}

existing = render(root, overrides: {"storage" => {"filesystem" => {"existingClaim" => "operator-data"}}})
abort "existing claim must not be managed" if existing.any? { |doc| doc["kind"] == "PersistentVolumeClaim" }
existing_volumes = resource(existing, "Deployment", "coyote-server").dig("spec", "template", "spec", "volumes")
abort "existing claim not mounted" unless existing_volumes.any? { |entry| entry.dig("persistentVolumeClaim", "claimName") == "operator-data" }
explicit_class = render(root, overrides: {"storage" => {"filesystem" => {"storageClassName" => "", "accessModes" => ["ReadWriteOncePod"]}}})
abort "explicit empty storage class lost" unless resource(explicit_class, "PersistentVolumeClaim", "coyote-data").dig("spec", "storageClassName") == ""

with_consumers = render(root, overrides: {"migration" => {"render" => true, "runID" => "portable"}, "mcp" => {"enabled" => true}, "images" => {"mcp" => "example.invalid/mcp@sha256:" + "e" * 64}})
[["Job", "coyote-migrate-portable"], ["Deployment", "coyote-mcp"]].each do |kind, name|
  consumer = resource(with_consumers, kind, name).dig("spec", "template", "spec")
  abort "#{name} must not mount a service-account token" unless consumer["automountServiceAccountToken"] == false
  abort "#{name} must not run a proxy" if consumer["initContainers"] || consumer.fetch("containers").length != 1
  sources = consumer.fetch("volumes").first.dig("projected", "sources")
  abort "#{name} must receive only the database" unless sources.length == 1 && sources.first.dig("secret", "name") == "coyote-database"
end

worker = render(root, worker: true)
worker_pod = resource(worker, "Deployment", "coyote-kubernetes-worker").dig("spec", "template", "spec")
abort "worker must receive only its database volume" unless worker_pod.fetch("volumes").length == 1 && worker_pod.fetch("volumes").first.dig("secret", "items") == [{"key" => "url", "path" => "database-url"}]
abort "worker must not receive the artifact PVC" if worker_pod.fetch("volumes").any? { |entry| entry.key?("persistentVolumeClaim") }
worker_extras = render(root, worker: true, overrides: {"serviceAccounts" => {"worker" => {"annotations" => {"example.org/identity" => "worker"}}}, "worker" => {"extraEnvSecrets" => {"GITHUB_STATUS_TOKEN" => {"name" => "github", "key" => "token"}}}})
abort "worker annotations missing" unless resource(worker_extras, "ServiceAccount", "coyote-kubernetes-worker-staging").dig("metadata", "annotations", "example.org/identity") == "worker"

%w[databaseUrl workspaceHelperCapability oidcClient session].each do |key|
  %w[name key].each do |field|
    render(root, overrides: {"secrets" => {"existingSecrets" => {key => {field => ""}}}}, failure: "requires name and key")
  end
end
render(root, worker: true, overrides: {"secrets" => {"existingSecrets" => {"databaseUrl" => {"name" => ""}}}}, failure: "requires name and key")
[
  [{"server" => {"replicas" => 2}}, "at most one"],
  [{"server" => {"replicasManagedExternally" => true}}, "at most one"],
  [{"storage" => {"provider" => "s3"}}, "storage/provider"],
  [{"storage" => {"filesystem" => {"accessModes" => ["ReadWriteMany"]}}}, "accessModes"],
  [{"storage" => {"filesystem" => {"securityContext" => {"runAsUser" => 0}}}}, "runAsUser"],
  [{"storage" => {"provider" => "gcs"}}, "bucket is required"],
  [{"cloudSql" => {"enabled" => true}}, "requires GKE workload identity"],
  [{"gke" => {"secretManagerCSI" => {"enabled" => true}}}, "explicit gke.enabled"],
  [{"serviceAccounts" => {"mcp" => {"gsaEmail" => "legacy@example.invalid"}}}, "opt-ins"],
  [{"secrets" => {"existingSecrets" => {"databaseUrl" => {"value" => "must-not-render"}}}}, "additional properties"],
  [{"server" => {"extraEnvSecrets" => {"DATABASE_URL_FILE" => {"name" => "db", "key" => "url"}}}}, "must not override"]
].each { |overrides, failure| render(root, overrides: overrides, failure: failure) }
render(root, worker: true, overrides: {"cloudBuild" => {"enabled" => true}}, failure: "requires GCS artifact storage")
render(root, worker: true, overrides: {"worker" => {"extraEnvSecrets" => {"CLOUD_BUILD_PROJECT" => {"name" => "cloud", "key" => "project"}}}}, failure: "must not override")

[false, true].each do |worker_chart|
  render(root, worker: worker_chart, gke: true, overrides: {"cloudSql" => {"enabled" => false}}, failure: "explicit cloudSql.enabled")
  render(root, worker: worker_chart, gke: true, overrides: {"gke" => {"workloadIdentity" => {"enabled" => false}}}, failure: "workload identity")
  render(root, worker: worker_chart, gke: true, overrides: {"gke" => {"secretManagerCSI" => {"enabled" => false}}}, failure: "CSI opt-ins")
  render(root, worker: worker_chart, gke: true, overrides: {"storage" => {"provider" => "filesystem"}, "server" => {"replicasManagedExternally" => false}}, failure: "explicit storage.provider=gcs")
end
render(root, worker: true, gke: true, overrides: {"cloudBuild" => {"enabled" => false}}, failure: "explicit cloudBuild.enabled")
puts "Portable Helm configuration tests passed"
RUBY

echo "Helm schema-negative tests passed"
