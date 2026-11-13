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
  local image="docker.io/library/coyote-helm-$name" archive="$work_dir/$name.tar"
  local build_args=(--platform "linux/$architecture" --provenance=false --tag "$image:smoke" --output "type=oci,dest=$archive")
  [[ -z "$target" ]] || build_args+=(--target "$target")
  docker buildx build "${build_args[@]}" "$context" >&2
  docker exec -i "$cluster-control-plane" ctr --namespace k8s.io images import --digests - <"$archive" >&2
  local digest
  digest="$(tar -xOf "$archive" index.json | jq -r '.manifests[0].digest')"
  docker exec "$cluster-control-plane" ctr --namespace k8s.io images tag "$image:smoke" "$image@$digest" >&2
  printf '%s@%s' "$image" "$digest"
}
server_image="$(load_image server "$repo_root/backend" source-runtime)"
migrate_image="$(load_image migrate "$repo_root/backend" migrate-runtime)"
frontend_image="$(load_image frontend "$repo_root/frontend" '')"

kubectl create namespace "$namespace"
kubectl create namespace portable-execution
openssl rand -hex 32 >"$work_dir/password"
printf 'postgres://coyote:%s@postgres.portable-control.svc.cluster.local:5432/coyote?sslmode=disable' "$(cat "$work_dir/password")" >"$work_dir/url"
openssl rand -hex 32 >"$work_dir/helper"
openssl rand -hex 32 >"$work_dir/session"
openssl rand -hex 32 >"$work_dir/client-secret"
printf 'coyote_pat_%s' "$(openssl rand -hex 32)" >"$work_dir/token"
printf 'Authorization: Bearer %s' "$(cat "$work_dir/token")" >"$work_dir/header"
printf /test-ca/tls.crt >"$work_dir/trust-path"
kubectl -n "$namespace" create secret generic postgres --from-file=password="$work_dir/password"
for target_namespace in "$namespace" portable-execution; do
  kubectl -n "$target_namespace" create secret generic coyote-database --from-file=url="$work_dir/url"
done
kubectl -n "$namespace" create secret generic coyote-runtime --from-file=helper="$work_dir/helper" --from-file=session="$work_dir/session" --from-file=trust-path="$work_dir/trust-path" --from-file=header="$work_dir/header"
kubectl -n "$namespace" create secret generic coyote-oidc --from-file=client-secret="$work_dir/client-secret"

issuer="https://oidc.$namespace.svc.cluster.local"
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=oidc \
  -addext "subjectAltName=DNS:oidc.$namespace.svc.cluster.local" \
  -keyout "$work_dir/tls.key" -out "$work_dir/tls.crt" >"$work_dir/openssl.log" 2>&1
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
jq -n --arg server "$server_image" --arg frontend "$frontend_image" --arg migrate "$migrate_image" --arg issuer "$issuer" \
  '{images: {server: $server, frontend: $frontend, migrate: $migrate}, auth: {oidc: {issuerURL: $issuer}}, server: {extraEnvSecrets: {SSL_CERT_FILE: {name: "coyote-runtime", key: "trust-path"}}}}' >"$work_dir/images.json"
control_args=(portable "$repo_root/deploy/helm/coyote-ci" --namespace "$namespace" --values "$repo_root/deploy/helm/examples/generic-control-plane-values.yaml" --values "$work_dir/images.json")
helm upgrade --install "${control_args[@]}" --set server.replicas=0 --set frontend.replicas=0 \
  --set migration.render=true --set migration.runID=portable --wait --wait-for-jobs --timeout 5m
helm upgrade "${control_args[@]}" --set migration.render=false
kubectl -n "$namespace" patch deployment coyote-server --type=strategic --patch \
  '{"spec":{"template":{"spec":{"volumes":[{"name":"test-ca","secret":{"secretName":"test-oidc-tls","items":[{"key":"tls.crt","path":"tls.crt"}]}}],"containers":[{"name":"server","volumeMounts":[{"name":"test-ca","mountPath":"/test-ca","readOnly":true}]}]}}}}'
kubectl -n "$namespace" rollout status deployment/coyote-server --timeout=180s
kubectl -n "$namespace" rollout status deployment/coyote-frontend --timeout=180s
helm upgrade --install portable-worker "$repo_root/deploy/helm/coyote-ci-worker" --namespace portable-execution \
  --values "$repo_root/deploy/helm/examples/generic-worker-values.yaml" --set-string "images.worker=$server_image" --wait --timeout 3m

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
helm uninstall portable --namespace "$namespace"
[[ "$(kubectl -n "$namespace" get pvc coyote-data -o jsonpath='{.metadata.uid}')" == "$claim_uid" ]]
echo "Portable Helm smoke passed: PostgreSQL, native Secrets, OIDC readiness, non-root filesystem persistence, authenticated download, PVC retention"