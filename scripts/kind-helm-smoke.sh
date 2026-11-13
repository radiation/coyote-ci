#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="$(mktemp -d /tmp/coyote-helm-smoke.XXXXXX)"
cluster="coyote-helm-$(date +%s)-$$"
namespace=portable-control
created=false
export KUBECONFIG="$work_dir/kubeconfig"

cleanup() {
  local result=$?
  if [[ "$created" == true ]]; then
    if [[ "$result" != 0 ]]; then
      kubectl -n "$namespace" get pods,pvc,jobs >&2 || true
      kubectl -n "$namespace" get jobs -l app.kubernetes.io/name=coyote-migrate -o name | while read -r job; do
        kubectl -n "$namespace" logs "$job" -c migrate --tail=80 >&2 || true
      done
      kubectl -n portable-execution get pods >&2 || true
      kubectl -n portable-execution logs deployment/coyote-kubernetes-worker --tail=40 >&2 || true
      kubectl -n "$namespace" logs deployment/coyote-server --tail=40 >&2 || true
    fi
    if [[ "${KIND_HELM_KEEP_CLUSTER:-false}" != true ]]; then
      kind delete cluster --name "$cluster"
    fi
  fi
  echo "Smoke evidence: $work_dir (contains disposable test credentials; delete when finished)"
}
trap cleanup EXIT
for command in docker kind kubectl helm jq openssl tar shasum; do
  command -v "$command" >/dev/null || { echo "$command is required" >&2; exit 1; }
done
kind create cluster --name "$cluster" --kubeconfig "$KUBECONFIG" --wait 120s
created=true
architecture="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.architecture}')"

load_image() {
  local name="$1" context="$2" target="$3"
  shift 3
  local image="docker.io/library/coyote-helm-$name" archive="$work_dir/$name.tar"
  local build_args=(--platform "linux/$architecture" --provenance=false --tag "$image:smoke" --output "type=oci,dest=$archive")
  build_args+=("$@")
  [[ -z "$target" ]] || build_args+=(--target "$target")
  docker buildx build "${build_args[@]}" "$context" >&2
  docker exec -i "$cluster-control-plane" ctr --namespace k8s.io images import --digests - <"$archive" >&2
  local digest
  digest="$(tar -xOf "$archive" index.json | jq -r '.manifests[0].digest')"
  docker exec "$cluster-control-plane" ctr --namespace k8s.io images tag "$image:smoke" "$image@$digest" >&2
  printf '%s@%s' "$image" "$digest"
}
migrate_image="$(load_image migrate "$repo_root/backend" migrate-runtime)"
bash "$repo_root/scripts/test-helm-hook-transition.sh" "$work_dir" "$migrate_image"
if [[ "${KIND_HELM_HOOK_TRANSITION_ONLY:-false}" == true ]]; then
  exit 0
fi
frontend_image="$(load_image frontend "$repo_root/frontend" '')"
schema_target="$(cd "$repo_root" && go run ./backend/cmd/migrate -dir backend/db/migrations target)"

kubectl create namespace "$namespace"
kubectl create namespace portable-execution
control_namespace_uid="$(kubectl get namespace "$namespace" -o jsonpath='{.metadata.uid}')"
execution_namespace_uid="$(kubectl get namespace portable-execution -o jsonpath='{.metadata.uid}')"
openssl rand -hex 32 >"$work_dir/password"
printf 'postgres://coyote:%s@postgres.portable-control.svc.cluster.local:5432/coyote?sslmode=disable' "$(cat "$work_dir/password")" >"$work_dir/url"
openssl rand -hex 32 >"$work_dir/helper"
openssl rand -hex 32 >"$work_dir/session"
openssl rand -hex 32 >"$work_dir/client-secret"
printf 'coyote_pat_%s' "$(openssl rand -hex 32)" >"$work_dir/token"
printf 'Authorization: Bearer %s' "$(cat "$work_dir/token")" >"$work_dir/header"
kubectl -n "$namespace" create secret generic postgres --from-file=password="$work_dir/password"
for target_namespace in "$namespace" portable-execution; do
  kubectl -n "$target_namespace" create secret generic coyote-database --from-file=url="$work_dir/url"
done
control_secret_uid="$(kubectl -n "$namespace" get secret coyote-database -o jsonpath='{.metadata.uid}')"
worker_secret_uid="$(kubectl -n portable-execution get secret coyote-database -o jsonpath='{.metadata.uid}')"
kubectl -n "$namespace" create secret generic coyote-runtime --from-file=helper="$work_dir/helper" --from-file=session="$work_dir/session" --from-file=header="$work_dir/header"
kubectl -n "$namespace" create secret generic coyote-oidc --from-file=client-secret="$work_dir/client-secret"

issuer="https://oidc.$namespace.svc.cluster.local"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=oidc \
  -addext "subjectAltName=DNS:oidc.$namespace.svc.cluster.local" \
  -keyout "$work_dir/tls.key" -out "$work_dir/tls.crt" >"$work_dir/openssl.log" 2>&1
cat "$repo_root/backend/Dockerfile" >"$work_dir/Dockerfile.smoke"
cat >>"$work_dir/Dockerfile.smoke" <<'EOF'

FROM source-runtime AS smoke-runtime
COPY --from=fixture tls.crt /usr/local/share/ca-certificates/test-oidc.crt
RUN update-ca-certificates
FROM migrate-runtime AS smoke-migrate
COPY --from=fixture pending.sql /migrations/
EOF
server_image="$(load_image server "$repo_root/backend" smoke-runtime --file "$work_dir/Dockerfile.smoke" --build-context "fixture=$work_dir")"
kubectl -n "$namespace" create secret tls test-oidc-tls --cert="$work_dir/tls.crt" --key="$work_dir/tls.key"
jq -n --arg issuer "$issuer" '{issuer: $issuer, authorization_endpoint: ($issuer + "/auth"), token_endpoint: ($issuer + "/token"), jwks_uri: ($issuer + "/keys"), id_token_signing_alg_values_supported: ["RS256"]}' >"$work_dir/discovery.json"
cat >"$work_dir/default.conf" <<'EOF'
server {
  listen 443 ssl;
  ssl_certificate /tls/tls.crt;
  ssl_certificate_key /tls/tls.key;
  location = /.well-known/openid-configuration {
    default_type application/json;
    alias /fixture/discovery.json;
  }
}
EOF
kubectl -n "$namespace" create configmap test-oidc --from-file="$work_dir/default.conf" --from-file="$work_dir/discovery.json"
kubectl -n "$namespace" apply -f - <<'EOF'
apiVersion: v1
kind: Pod
metadata: {name: postgres, labels: {app: postgres}}
spec:
  automountServiceAccountToken: false
  containers:
    - name: postgres
      image: postgres:16-alpine
      env:
        - {name: POSTGRES_USER, value: coyote}
        - {name: POSTGRES_DB, value: coyote}
        - {name: POSTGRES_PASSWORD, valueFrom: {secretKeyRef: {name: postgres, key: password}}}
      readinessProbe: {exec: {command: [pg_isready, -U, coyote, -d, coyote]}, periodSeconds: 2}
---
apiVersion: v1
kind: Service
metadata: {name: postgres}
spec: {selector: {app: postgres}, ports: [{port: 5432}]}
---
apiVersion: v1
kind: Pod
metadata: {name: oidc, labels: {app: oidc}}
spec:
  automountServiceAccountToken: false
  containers:
    - name: oidc
      image: nginx:alpine
      volumeMounts:
        - {name: config, mountPath: /etc/nginx/conf.d, readOnly: true}
        - {name: fixture, mountPath: /fixture, readOnly: true}
        - {name: tls, mountPath: /tls, readOnly: true}
      readinessProbe: {tcpSocket: {port: 443}, periodSeconds: 2}
  volumes:
    - {name: config, configMap: {name: test-oidc, items: [{key: default.conf, path: default.conf}]}}
    - {name: fixture, configMap: {name: test-oidc, items: [{key: discovery.json, path: discovery.json}]}}
    - {name: tls, secret: {secretName: test-oidc-tls}}
---
apiVersion: v1
kind: Service
metadata: {name: oidc}
spec: {selector: {app: oidc}, ports: [{port: 443}]}
EOF
kubectl -n "$namespace" wait --for=condition=Ready pod/postgres pod/oidc --timeout=180s
jq -n --arg server "$server_image" --arg frontend "$frontend_image" --arg migrate "$migrate_image" --arg issuer "$issuer" --arg target "$schema_target" \
  '{images: {server: $server, worker: $server, frontend: $frontend, migrate: $migrate}, auth: {oidc: {issuerURL: $issuer}}, release: {version: "1.0.0"}, compatibility: {schema_target: $target, upgrade_from: ">=1.0.0 <2.0.0"}}' >"$work_dir/images.json"
control_args=(portable "$repo_root/deploy/helm/coyote-ci" --namespace "$namespace" --values "$repo_root/deploy/helm/examples/generic-values.yaml" --values "$work_dir/images.json")
if [[ "${KIND_HELM_LEGACY_BRIDGE:-false}" == true ]]; then
  previous_chart="$work_dir/hook-transition/previous/deploy/helm/coyote-ci"
  helm install portable "$previous_chart" --namespace "$namespace" \
    --values "$repo_root/deploy/helm/examples/generic-values.yaml" --values "$work_dir/images.json" \
    --set server.replicas=0 --set frontend.replicas=0 --set worker.replicas=0 --set mcp.replicas=0 --set migration.render=false
  kubectl -n "$namespace" get serviceaccount coyote-migrate -o json | jq -e '.metadata.annotations["meta.helm.sh/release-name"] == "portable" and .metadata.annotations["helm.sh/hook"] == null' >/dev/null
  bridge_claim_uid="$(kubectl -n "$namespace" get pvc coyote-data -o jsonpath='{.metadata.uid}')"
  helm get manifest portable -n "$namespace" >"$work_dir/previous-full-manifest.yaml"
  mkdir -p "$work_dir/bridge/templates"
  helm show chart "$previous_chart" >"$work_dir/bridge/Chart.yaml"
  ruby -ryaml - "$work_dir/previous-full-manifest.yaml" "$work_dir/bridge/templates/resources.yaml" <<'RUBY'
documents = YAML.load_stream(File.read(ARGV[0])).compact
removed = documents.select { |document| [["ServiceAccount", "coyote-migrate"], ["SecretProviderClass", "coyote-migrate-secrets"]].include?([document["kind"], document.dig("metadata", "name")]) }
abort "expected the native migration ServiceAccount" unless removed.length == 1
File.write(ARGV[1], (documents - removed).map(&:to_yaml).join)
RUBY
  helm upgrade portable "$work_dir/bridge" --namespace "$namespace"
  [[ -z "$(kubectl -n "$namespace" get serviceaccount coyote-migrate --ignore-not-found -o name)" ]] || exit 1
  helm get manifest portable -n "$namespace" >"$work_dir/bridge-full-manifest.yaml"
  ruby -ryaml - "$work_dir/previous-full-manifest.yaml" "$work_dir/bridge-full-manifest.yaml" <<'RUBY'
previous, bridge = ARGV.map { |path| YAML.load_stream(File.read(path)).compact }
expected = previous.reject { |document| document["kind"] == "ServiceAccount" && document.dig("metadata", "name") == "coyote-migrate" }
key = ->(document) { [document["kind"], document.dig("metadata", "name")] }
abort "bridge changed unrelated resources" unless expected.sort_by(&key) == bridge.sort_by(&key)
RUBY
fi
helm upgrade --install "${control_args[@]}" --wait --timeout 15m
initial_revision="$(helm history portable -n "$namespace" -o json | jq -r 'last.revision')"
kubectl -n "$namespace" get job "coyote-migrate-portable-$initial_revision" -o json >"$work_dir/initial-migration.json"
kubectl -n "$namespace" get deployment coyote-server -o json >"$work_dir/initial-server.json"
kubectl -n "$namespace" get serviceaccount coyote-migrate -o json | jq -e '.metadata.annotations["helm.sh/hook"] == "pre-install,pre-upgrade"' >/dev/null
if [[ "${KIND_HELM_LEGACY_BRIDGE:-false}" == true ]]; then
  [[ "$(kubectl -n "$namespace" get pvc coyote-data -o jsonpath='{.metadata.uid}')" == "$bridge_claim_uid" ]] || exit 1
  kubectl -n "$namespace" get pods -l app.kubernetes.io/name=coyote-server -o json >"$work_dir/initial-server-pods.json"
  jq -e -s '.[0].status.completionTime as $completed | all(.[1].items[]; .metadata.creationTimestamp >= $completed)' "$work_dir/initial-migration.json" "$work_dir/initial-server-pods.json" >/dev/null
else
  jq -e -s '.[0].status.completionTime <= .[1].metadata.creationTimestamp' "$work_dir/initial-migration.json" "$work_dir/initial-server.json" >/dev/null
fi
ledger_count="$(kubectl -n "$namespace" exec postgres -- psql -U coyote -d coyote -Atc 'SELECT count(*) FROM goose_db_version')"
kubectl -n "$namespace" annotate configmap coyote-control-plane coyote-ci/release-version-
helm upgrade "${control_args[@]}" --wait --timeout 15m
kubectl -n "$namespace" get serviceaccount coyote-migrate -o json | jq -e '.metadata.annotations["helm.sh/hook"] == "pre-install,pre-upgrade"' >/dev/null
[[ "$(kubectl -n "$namespace" exec postgres -- psql -U coyote -d coyote -Atc 'SELECT count(*) FROM goose_db_version')" == "$ledger_count" ]]
kubectl -n "$namespace" apply -f - <<'EOF'
apiVersion: batch/v1
kind: Job
metadata:
  name: migration-busy-fixture
  labels: {app.kubernetes.io/name: coyote-migrate, app.kubernetes.io/instance: portable}
spec:
  suspend: true
  template:
    spec:
      restartPolicy: Never
      containers: [{name: fixture, image: busybox:1.37, command: ["true"]}]
EOF
if helm upgrade "${control_args[@]}" --dry-run=server >"$work_dir/busy-job.log" 2>&1; then
  echo "Expected active migration Job rejection" >&2
  exit 1
fi
grep -q 'earlier migration Job is not terminal' "$work_dir/busy-job.log"
kubectl -n "$namespace" delete job migration-busy-fixture
if helm upgrade "${control_args[@]}" --set-string compatibility.schema_target=99999 --wait --timeout 15m >"$work_dir/target-mismatch.log" 2>&1; then
  echo "Expected image/target mismatch rejection" >&2
  exit 1
fi
[[ "$(kubectl -n "$namespace" exec postgres -- psql -U coyote -d coyote -Atc 'SELECT count(*) FROM goose_db_version')" == "$ledger_count" ]]
helm upgrade "${control_args[@]}" --wait --timeout 15m
kubectl -n "$namespace" rollout status deployment/coyote-server --timeout=180s
kubectl -n "$namespace" rollout status deployment/coyote-frontend --timeout=180s
kubectl -n portable-execution rollout status deployment/coyote-kubernetes-worker --timeout=180s
[[ "$(helm list --all-namespaces --output json | jq length)" == 1 ]]
[[ "$(kubectl -n portable-execution get deployment coyote-kubernetes-worker -o jsonpath='{.metadata.annotations.meta\.helm\.sh/release-namespace}')" == "$namespace" ]]
worker_uid="$(kubectl -n portable-execution get deployment coyote-kubernetes-worker -o jsonpath='{.metadata.uid}')"
[[ "$(kubectl auth can-i create jobs.batch -n portable-execution --as=system:serviceaccount:portable-execution:coyote-kubernetes-worker)" == yes ]]
[[ "$(kubectl auth can-i create jobs.batch -n "$namespace" --as=system:serviceaccount:portable-execution:coyote-kubernetes-worker)" == no ]]
[[ "$(kubectl auth can-i get secrets -n portable-execution --as=system:serviceaccount:portable-execution:coyote-kubernetes-worker)" == no ]]
[[ "$(kubectl auth can-i get pods -n portable-execution --as=system:serviceaccount:portable-execution:coyote-workspace-helper)" == yes ]]
[[ "$(kubectl auth can-i get pods -n "$namespace" --as=system:serviceaccount:portable-execution:coyote-workspace-helper)" == no ]]
[[ "$(kubectl auth can-i get pods -n portable-execution --as="system:serviceaccount:$namespace:coyote-server")" == yes ]]

printf 'portable Helm artifact persistence\n' >"$work_dir/expected"
checksum="$(shasum -a 256 "$work_dir/expected" | cut -d ' ' -f 1)"
token_hash="$(shasum -a 256 "$work_dir/token" | cut -d ' ' -f 1)"
kubectl -n "$namespace" exec -i postgres -- psql -U coyote -d coyote -v ON_ERROR_STOP=1 >"$work_dir/seed.log" <<EOF
INSERT INTO users (id,email,global_role,created_at,updated_at) VALUES ('00000000-0000-0000-0000-000000000010','smoke@example.invalid','user',now(),now());
INSERT INTO project_memberships (project_id,user_id,role,created_at,updated_at) VALUES ('00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000010','viewer',now(),now());
INSERT INTO api_tokens (id,user_id,name,token_hash,token_prefix,scopes,expires_at,created_at,updated_at) VALUES ('00000000-0000-0000-0000-000000000011','00000000-0000-0000-0000-000000000010','smoke','$token_hash','coyote_pat_','["artifact:read"]',now()+interval '1 hour',now(),now());
INSERT INTO builds (id,project_id,status,build_number,created_at) VALUES ('00000000-0000-0000-0000-000000000012','00000000-0000-0000-0000-000000000001','success',1,now());
INSERT INTO artifact_packages (id,project_id,scope_build_id,logical_path) VALUES ('00000000-0000-0000-0000-000000000013','00000000-0000-0000-0000-000000000001','00000000-0000-0000-0000-000000000012','smoke.txt');
INSERT INTO build_artifacts (id,build_id,package_id,logical_path,storage_key,storage_provider,size_bytes,checksum_sha256) VALUES ('00000000-0000-0000-0000-000000000014','00000000-0000-0000-0000-000000000012','00000000-0000-0000-0000-000000000013','smoke.txt','smoke/smoke.txt','filesystem',$(wc -c <"$work_dir/expected"),'$checksum');
EOF
kubectl -n "$namespace" exec -i deployment/coyote-server -c server -- sh -ec \
  'test "$(id -u)" = 10001; umask 077; mkdir -p "$ARTIFACT_STORAGE_ROOT/smoke"; cat >"$ARTIFACT_STORAGE_ROOT/smoke/smoke.txt"' <"$work_dir/expected"
claim_uid="$(kubectl -n "$namespace" get pvc coyote-data -o jsonpath='{.metadata.uid}')"
pod_uid="$(kubectl -n "$namespace" get pod -l app.kubernetes.io/name=coyote-server -o jsonpath='{.items[0].metadata.uid}')"
for phase in before after; do
  if [[ "$phase" == after ]]; then
    next_target="$((schema_target + 1))"
    printf '%s\n' '-- +goose Up' 'CREATE TABLE helm_migration_evidence (id integer);' 'SELECT 1 / count(*) FROM helm_retry_gate;' >"$work_dir/pending.sql"
    sed "s|COPY --from=fixture pending.sql /migrations/|COPY --from=fixture pending.sql /migrations/$(printf '%05d' "$next_target")_helm_smoke.sql|" "$work_dir/Dockerfile.smoke" >"$work_dir/Dockerfile.upgrade"
    upgrade_image="$(load_image migrate-upgrade "$repo_root/backend" smoke-migrate --file "$work_dir/Dockerfile.upgrade" --build-context "fixture=$work_dir")"
    jq -n --arg image "$upgrade_image" --arg target "$next_target" '{images: {migrate: $image}, release: {version: "1.1.0"}, compatibility: {schema_target: $target}, migration: {onlineUpgradeApproved: true}}' >"$work_dir/upgrade.json"
    control_args+=(--values "$work_dir/upgrade.json")
    kubectl -n "$namespace" exec postgres -- psql -U coyote -d coyote -c 'CREATE TABLE helm_retry_gate (id integer)'
    generation="$(kubectl -n "$namespace" get deployment coyote-server -o jsonpath='{.metadata.generation}')"
    if helm upgrade "${control_args[@]}" --set worker.replicas=2 --wait --timeout 15m; then
      echo "Expected transactional migration failure" >&2
      exit 1
    fi
    [[ "$(kubectl -n "$namespace" get deployment coyote-server -o jsonpath='{.metadata.generation}')" == "$generation" ]]
    [[ "$(kubectl -n portable-execution get deployment coyote-kubernetes-worker -o jsonpath='{.spec.replicas}')" == 1 ]]
    kubectl -n "$namespace" get jobs -l app.kubernetes.io/name=coyote-migrate -o json | jq -e 'any(.items[]; any(.status.conditions[]?; .type == "Failed" and .status == "True"))' >/dev/null
    [[ "$(kubectl -n "$namespace" exec postgres -- psql -U coyote -d coyote -Atc "SELECT to_regclass('helm_migration_evidence') IS NULL")" == t ]]
    kubectl -n "$namespace" exec postgres -- psql -U coyote -d coyote -c 'INSERT INTO helm_retry_gate VALUES (1)'
    helm upgrade "${control_args[@]}" --set worker.replicas=2 --wait --timeout 15m
    [[ "$(kubectl -n "$namespace" exec postgres -- psql -U coyote -d coyote -Atc 'SELECT max(version_id) FROM goose_db_version WHERE is_applied')" == "$next_target" ]]
    [[ "$(kubectl -n portable-execution get deployment coyote-kubernetes-worker -o jsonpath='{.metadata.uid}')" == "$worker_uid" ]]
    [[ "$(kubectl -n portable-execution get deployment coyote-kubernetes-worker -o jsonpath='{.spec.replicas}')" == 2 ]]
    kubectl -n "$namespace" rollout restart deployment/coyote-server
    kubectl -n "$namespace" rollout status deployment/coyote-server --timeout=180s
    [[ "$(kubectl -n "$namespace" get pod -l app.kubernetes.io/name=coyote-server -o jsonpath='{.items[0].metadata.uid}')" != "$pod_uid" ]]
    [[ "$(kubectl -n "$namespace" get pvc coyote-data -o jsonpath='{.metadata.uid}')" == "$claim_uid" ]]
  fi
  kubectl -n "$namespace" run "download-$phase" --image=curlimages/curl:8.12.1 --restart=Never --overrides \
    '{"spec":{"automountServiceAccountToken":false,"volumes":[{"name":"auth","secret":{"secretName":"coyote-runtime","items":[{"key":"header","path":"header"}]}}],"containers":[{"name":"download","image":"curlimages/curl:8.12.1","args":["--fail","--silent","--show-error","--header","@/auth/header","http://coyote-server:8080/api/builds/00000000-0000-0000-0000-000000000012/artifacts/00000000-0000-0000-0000-000000000014/download"],"volumeMounts":[{"name":"auth","mountPath":"/auth","readOnly":true}]}]}}'
  kubectl -n "$namespace" wait --for=jsonpath='{.status.phase}'=Succeeded "pod/download-$phase" --timeout=120s
  kubectl -n "$namespace" logs "download-$phase" >"$work_dir/download-$phase"
  [[ "$(shasum -a 256 "$work_dir/download-$phase" | cut -d ' ' -f 1)" == "$checksum" ]]
done
helm upgrade "${control_args[@]}" --set worker.enabled=false --wait --timeout 3m
[[ "$(kubectl -n portable-execution get deployment coyote-kubernetes-worker --ignore-not-found -o name)" == "" ]]
[[ "$(kubectl -n portable-execution get serviceaccount coyote-workspace-helper --ignore-not-found -o name)" == "" ]]
helm upgrade "${control_args[@]}" --wait --timeout 3m
kubectl -n portable-execution rollout status deployment/coyote-kubernetes-worker --timeout=180s
helm uninstall portable --namespace "$namespace"
[[ "$(kubectl -n "$namespace" get pvc coyote-data -o jsonpath='{.metadata.uid}')" == "$claim_uid" ]]
[[ "$(kubectl get namespace "$namespace" -o jsonpath='{.metadata.uid}')" == "$control_namespace_uid" ]]
[[ "$(kubectl get namespace portable-execution -o jsonpath='{.metadata.uid}')" == "$execution_namespace_uid" ]]
[[ "$(kubectl -n "$namespace" get secret coyote-database -o jsonpath='{.metadata.uid}')" == "$control_secret_uid" ]]
[[ "$(kubectl -n portable-execution get secret coyote-database -o jsonpath='{.metadata.uid}')" == "$worker_secret_uid" ]]
kubectl -n "$namespace" get secret coyote-runtime coyote-oidc >/dev/null
[[ "$(kubectl -n portable-execution get deployments,roles,rolebindings -o json | jq '.items | length')" == 0 ]]
[[ "$(kubectl -n portable-execution get serviceaccount coyote-kubernetes-worker coyote-workspace-helper --ignore-not-found -o name)" == "" ]]
[[ "$(helm list --all-namespaces --output json | jq length)" == 0 ]]
echo "Unified Helm smoke passed: automatic install, no-op, pending migration failure/retry, rollout ordering, worker toggle, scoped RBAC, authenticated persistence, uninstall, Secret/PVC/namespace retention"