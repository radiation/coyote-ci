#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
control_namespace="${GKE_HELM_CONTROL_PLANE_NAMESPACE:-coyote-ci}"
worker_namespace="${GKE_HELM_WORKER_NAMESPACE:-coyote-ci-staging}"
control_values="${GKE_HELM_CONTROL_PLANE_VALUES:-$repo_root/.local/helm/gke-staging-control-plane-values.yaml}"
worker_values="${GKE_HELM_WORKER_VALUES:-$repo_root/.local/helm/gke-staging-worker-values.yaml}"
migrate_image="${COYOTE_MIGRATE_IMAGE:-}"
overwrite="${GKE_HELM_VALUES_OVERWRITE:-false}"

require_command() {
  command -v "$1" >/dev/null 2>&1 || { echo "$1 is required" >&2; exit 1; }
}

require_command kubectl
require_command ruby

[[ "$control_namespace" == "coyote-ci" ]] || { echo "GKE_HELM_CONTROL_PLANE_NAMESPACE must be coyote-ci" >&2; exit 1; }
[[ "$worker_namespace" == "coyote-ci-staging" ]] || { echo "GKE_HELM_WORKER_NAMESPACE must be coyote-ci-staging" >&2; exit 1; }
[[ "$migrate_image" == *@sha256:* ]] || {
  echo "COYOTE_MIGRATE_IMAGE must be the digest-pinned image used by the current staging migration workflow" >&2
  echo "It cannot be recovered from steady-state Kubernetes resources because the migration Job is intentionally absent." >&2
  exit 1
}
[[ "$overwrite" == "true" || "$overwrite" == "false" ]] || { echo "GKE_HELM_VALUES_OVERWRITE must be true or false" >&2; exit 1; }
if [[ "$overwrite" != "true" && ( -e "$control_values" || -e "$worker_values" ) ]]; then
  echo "local values already exist; set GKE_HELM_VALUES_OVERWRITE=true to refresh them" >&2
  exit 1
fi

work_dir="$(mktemp -d)"
trap 'rm -rf "$work_dir"' EXIT

kubectl -n "$control_namespace" get deployment coyote-server -o json >"$work_dir/server.json"
kubectl -n "$control_namespace" get deployment coyote-frontend -o json >"$work_dir/frontend.json"
kubectl -n "$worker_namespace" get deployment coyote-kubernetes-worker-staging -o json >"$work_dir/worker.json"
kubectl -n "$control_namespace" get serviceaccount coyote-server coyote-migrate -o json >"$work_dir/control-serviceaccounts.json"
kubectl -n "$worker_namespace" get serviceaccount coyote-kubernetes-worker-staging -o json >"$work_dir/worker-serviceaccount.json"
kubectl -n "$control_namespace" get configmap coyote-control-plane -o json >"$work_dir/control-configmap.json"
kubectl -n "$worker_namespace" get configmap coyote-gke-staging-worker -o json >"$work_dir/worker-configmap.json"
kubectl -n "$control_namespace" get secretproviderclass coyote-server-secrets -o json >"$work_dir/control-secrets.json"
kubectl -n "$worker_namespace" get secretproviderclass coyote-staging-worker-database-secrets -o json >"$work_dir/worker-secrets.json"
kubectl -n "$control_namespace" get gateway coyote-temporary-public -o json >"$work_dir/gateway.json"

mkdir -p "$(dirname "$control_values")" "$(dirname "$worker_values")"
ruby -rjson -ryaml -e '
  server_path, frontend_path, worker_path, control_sa_path, worker_sa_path, control_config_path, worker_config_path, control_secrets_path, worker_secrets_path, gateway_path, migrate_image, control_output, worker_output = ARGV

  def document(path)
    JSON.parse(File.read(path))
  end

  def fail_value(name)
    abort "live staging resource is missing required #{name}"
  end

  def container(deployment, name)
    deployment.dig("spec", "template", "spec", "containers")&.find { |item| item["name"] == name } || fail_value("container #{name}")
  end

  def image(container, name)
    value = container["image"]
    fail_value("#{name} image") unless value&.include?("@sha256:")
    value
  end

  def resources(container, name)
    value = container["resources"]
    fail_value("#{name} resources") unless value.is_a?(Hash)
    value
  end

  def service_account_gsa(list, name)
    accounts = list["items"] || [list]
    account = accounts.find { |item| item.dig("metadata", "name") == name } || fail_value("ServiceAccount #{name}")
    account.dig("metadata", "annotations", "iam.gke.io/gcp-service-account") || fail_value("Workload Identity annotation for #{name}")
  end

  def env_values(deployment)
    container(deployment, "worker").fetch("env", []).each_with_object({}) do |item, result|
      result[item["name"]] = item["value"] if item.key?("value")
    end
  end

  def secret_references(secret_provider_class)
    YAML.load(secret_provider_class.dig("spec", "parameters", "secrets") || "").each_with_object({}) do |item, result|
      match = /\Aprojects\/([^\/]+)\/secrets\/([^\/]+)\/versions\/[^\/]+\z/.match(item.fetch("resourceName"))
      abort "unsupported SecretProviderClass resourceName #{item.fetch("resourceName").inspect}" unless match
      result[item.fetch("path")] = [match[1], match[2]]
    end
  end

  def required(map, key)
    map.fetch(key) { fail_value("ConfigMap key #{key}") }
  end

  server = document(server_path)
  frontend = document(frontend_path)
  worker = document(worker_path)
  control_config = document(control_config_path).fetch("data")
  worker_config = document(worker_config_path).fetch("data")
  control_secrets = secret_references(document(control_secrets_path))
  worker_secrets = secret_references(document(worker_secrets_path))
  server_container = container(server, "server")
  server_proxy = container(server, "cloud-sql-proxy")
  frontend_container = container(frontend, "frontend")
  worker_container = container(worker, "worker")
  worker_proxy = container(worker, "cloud-sql-proxy")
  worker_env = env_values(worker)
  gateway = document(gateway_path)

  cloud_sql_connection = server_proxy.fetch("args").last
  abort "control-plane and worker Cloud SQL connection names differ" unless cloud_sql_connection == worker_proxy.fetch("args").last
  abort "control-plane and worker Cloud SQL proxy resources differ" unless resources(server_proxy, "server Cloud SQL proxy") == resources(worker_proxy, "worker Cloud SQL proxy")
  secret_project = control_secrets.fetch("database-url").fetch(0)
  %w[workspace-helper-capability-secret oidc-client-secret session-secret].each do |path|
    abort "control SecretProviderClass project mismatch" unless control_secrets.fetch(path).fetch(0) == secret_project
  end
  abort "worker SecretProviderClass project mismatch" unless worker_secrets.fetch("database-url").fetch(0) == secret_project

  expected_internal_url = "http://coyote-server.#{server.dig("metadata", "namespace")}.svc.cluster.local:8080"
  abort "worker verifier API URL is not #{expected_internal_url}" unless worker_config["WORKER_KUBERNETES_INTERNAL_API_URL"] == expected_internal_url

  control_values = {
    "namespace" => server.dig("metadata", "namespace"),
    "executionNamespace" => worker.dig("metadata", "namespace"),
    "images" => {
      "server" => image(server_container, "server"),
      "frontend" => image(frontend_container, "frontend"),
      "migrate" => migrate_image,
      "cloudSqlProxy" => image(server_proxy, "server Cloud SQL proxy")
    },
    "serviceAccounts" => {
      "server" => {"name" => server.dig("spec", "template", "spec", "serviceAccountName"), "gsaEmail" => service_account_gsa(document(control_sa_path), "coyote-server")},
      "migrate" => {"name" => "coyote-migrate", "gsaEmail" => service_account_gsa(document(control_sa_path), "coyote-migrate")}
    },
    "cloudSql" => {"connectionName" => cloud_sql_connection, "proxy" => {"resources" => resources(server_proxy, "server Cloud SQL proxy")}},
    "secrets" => {
      "project" => secret_project,
      "databaseUrl" => control_secrets.fetch("database-url").fetch(1),
      "workspaceHelperCapability" => control_secrets.fetch("workspace-helper-capability-secret").fetch(1),
      "oidcClient" => control_secrets.fetch("oidc-client-secret").fetch(1),
      "session" => control_secrets.fetch("session-secret").fetch(1)
    },
    "storage" => {
      "project" => required(control_config, "ARTIFACT_GCS_PROJECT"),
      "artifacts" => {"bucket" => required(control_config, "ARTIFACT_GCS_BUCKET"), "prefix" => required(control_config, "ARTIFACT_GCS_PREFIX")},
      "cache" => {"bucket" => required(control_config, "WORKER_CACHE_GCS_BUCKET"), "prefix" => required(control_config, "WORKER_CACHE_GCS_PREFIX")},
      "workspaceRevisions" => {"bucket" => required(control_config, "WORKSPACE_REVISION_GCS_BUCKET"), "prefix" => required(control_config, "WORKSPACE_REVISION_GCS_PREFIX")}
    },
    "auth" => {
      "mode" => required(control_config, "AUTH_MODE"),
      "bootstrapAdminEmails" => required(control_config, "BOOTSTRAP_ADMIN_EMAILS"),
      "oidc" => {
        "issuerURL" => required(control_config, "OIDC_ISSUER_URL"),
        "clientID" => required(control_config, "OIDC_CLIENT_ID"),
        "redirectURL" => required(control_config, "OIDC_REDIRECT_URL"),
        "scopes" => required(control_config, "OIDC_SCOPES")
      },
      "publicURL" => required(control_config, "COYOTE_PUBLIC_URL")
    },
    "server" => {"replicas" => server.dig("spec", "replicas"), "resources" => resources(server_container, "server")},
    "frontend" => {"replicas" => frontend.dig("spec", "replicas"), "resources" => resources(frontend_container, "frontend")},
    "gateway" => {
      "enabled" => true,
      "name" => gateway.dig("metadata", "name"),
      "hostname" => gateway.dig("spec", "listeners", 0, "hostname"),
      "addressName" => gateway.dig("spec", "addresses", 0, "value"),
      "certificateMap" => gateway.dig("metadata", "annotations", "networking.gke.io/certmap"),
      "className" => gateway.dig("spec", "gatewayClassName")
    }
  }
  worker_values = {
    "namespace" => worker.dig("metadata", "namespace"),
    "controlPlaneNamespace" => server.dig("metadata", "namespace"),
    "images" => {"worker" => image(worker_container, "worker"), "cloudSqlProxy" => image(worker_proxy, "worker Cloud SQL proxy")},
    "serviceAccounts" => {
      "worker" => {"name" => worker.dig("spec", "template", "spec", "serviceAccountName"), "gsaEmail" => service_account_gsa(document(worker_sa_path), "coyote-kubernetes-worker-staging")}
    },
    "cloudSql" => {"connectionName" => cloud_sql_connection, "proxy" => {"resources" => resources(worker_proxy, "worker Cloud SQL proxy")}},
    "secrets" => {"project" => secret_project, "databaseUrl" => worker_secrets.fetch("database-url").fetch(1)},
    "storage" => {
      "project" => required(worker_env, "ARTIFACT_GCS_PROJECT"),
      "artifacts" => {"bucket" => required(worker_env, "ARTIFACT_GCS_BUCKET"), "prefix" => required(worker_env, "ARTIFACT_GCS_PREFIX")}
    },
    "cloudBuild" => {
      "project" => required(worker_env, "GOOGLE_CLOUD_PROJECT"),
      "location" => required(worker_env, "CLOUD_BUILD_LOCATION"),
      "runtimeServiceAccount" => required(worker_env, "CLOUD_BUILD_RUNTIME_SERVICE_ACCOUNT"),
      "artifactRegistryRepository" => required(worker_env, "CLOUD_BUILD_ARTIFACT_REGISTRY_REPOSITORY"),
      "source" => {"bucket" => required(worker_env, "CLOUD_BUILD_SOURCE_BUCKET"), "prefix" => required(worker_env, "CLOUD_BUILD_SOURCE_PREFIX")}
    },
    "worker" => {"replicas" => worker.dig("spec", "replicas"), "maxInFlightJobs" => Integer(required(worker_env, "WORKER_KUBERNETES_MAX_IN_FLIGHT_JOBS")), "resources" => resources(worker_container, "worker")}
  }
  File.write(control_output, YAML.dump(control_values))
  File.write(worker_output, YAML.dump(worker_values))
' \
  "$work_dir/server.json" "$work_dir/frontend.json" "$work_dir/worker.json" \
  "$work_dir/control-serviceaccounts.json" "$work_dir/worker-serviceaccount.json" \
  "$work_dir/control-configmap.json" "$work_dir/worker-configmap.json" \
  "$work_dir/control-secrets.json" "$work_dir/worker-secrets.json" "$work_dir/gateway.json" \
  "$migrate_image" "$control_values" "$worker_values"

echo "Wrote local staging values:"
echo "  $control_values"
echo "  $worker_values"
echo "No Secret objects or secret payloads were read or written."
