#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="${1:?test evidence directory is required}/hook-transition"
image="${2:?local migration image is required}"
[[ "$(kubectl config current-context)" == kind-coyote-helm-* ]] || { echo "Disposable Coyote Helm Kind context required" >&2; exit 1; }
mkdir -p "$work_dir/previous"
previous_revision="${KIND_HELM_PREVIOUS_REVISION:-9b116a23}"
git -C "$repo_root" archive "$previous_revision" deploy/helm/coyote-ci | tar -x -C "$work_dir/previous"
helm version --short >"$work_dir/helm-version.txt"
git -C "$repo_root" rev-parse "$previous_revision" >"$work_dir/previous-revision.txt"
kubectl apply -f - <<'EOF'
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: secretproviderclasses.secrets-store.csi.x-k8s.io
spec:
  group: secrets-store.csi.x-k8s.io
  scope: Namespaced
  names: {plural: secretproviderclasses, singular: secretproviderclass, kind: SecretProviderClass}
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec: {type: object, x-kubernetes-preserve-unknown-fields: true}
EOF
kubectl wait --for=condition=Established crd/secretproviderclasses.secrets-store.csi.x-k8s.io --timeout=60s

assert_hooks() {
  local expected_phase="$1" revision
  revision="$(helm history transition -n "$namespace" -o json | jq -r 'last.revision')"
  kubectl -n "$namespace" get secret "sh.helm.release.v1.transition.v$revision" -o json | ruby -rjson -rbase64 -rzlib -rstringio -e 'puts Zlib::GzipReader.new(StringIO.new(Base64.decode64(Base64.decode64(JSON.parse(STDIN.read).fetch("data").fetch("release"))))).read' >"$directory/revision-$revision.json"
  ruby -rjson -rtime - "$directory/revision-$revision.json" "$mode" "$expected_phase" <<'RUBY'
release = JSON.parse(File.read(ARGV[0]))
hooks = release.fetch("hooks").sort_by { |hook| hook.fetch("weight") }
expected = ARGV[1] == "csi" ? [-30, -20, -10] : [-30, -10]
abort "incorrect hook sequence" unless hooks.map { |hook| hook.fetch("weight") } == expected
hooks.each_with_index do |hook, index|
  run = hook.fetch("last_run")
  phase = index == hooks.length - 1 ? ARGV[2] : "Succeeded"
  abort "incorrect hook outcome" unless run.fetch("phase") == phase
  if index > 0
    abort "hooks executed out of order" unless Time.iso8601(hooks[index - 1].fetch("last_run").fetch("completed_at")) <= Time.iso8601(run.fetch("started_at"))
  end
end
RUBY
  kubectl -n "$namespace" get job "coyote-migrate-transition-$revision" -o json >"$directory/job-$revision.json"
  jq -e --arg phase "$expected_phase" 'any(.status.conditions[]; .status == "True" and .type == (if $phase == "Succeeded" then "Complete" else "Failed" end))' "$directory/job-$revision.json" >/dev/null
}

assert_prerequisites() {
  local expected_count="$1" label="$2"
  kubectl -n "$namespace" get "${resources[@]}" --ignore-not-found -o json >"$directory/$label.json"
  jq -s -e --argjson expected "$expected_count" '[.[] | if .kind == "List" then .items[] else . end] | length == $expected' "$directory/$label.json" >/dev/null || { echo "Unexpected prerequisite state: $label" >&2; exit 1; }
}

for mode in native csi; do
  for scenario in success failure bridge-success bridge-failure; do
    namespace="hook-$mode-$scenario"
    directory="$work_dir/$mode-$scenario"
    mkdir -p "$directory/old/templates" "$directory/new/templates" "$directory/bridge/templates"
    kubectl create namespace "$namespace"
    kubectl -n "$namespace" create secret generic coyote-database --from-literal=url=disposable-lifecycle-fixture
    values="$repo_root/deploy/helm/examples/generic-values.yaml"
    if [[ "$mode" == csi ]]; then
      values="$repo_root/deploy/helm/examples/gke-staging-control-plane-values.yaml"
    fi
    for version in old new; do
      chart="$repo_root/deploy/helm/coyote-ci"
      [[ "$version" != old ]] || chart="$work_dir/previous/deploy/helm/coyote-ci"
      helm template transition "$chart" --namespace "$namespace" --values "$values" --set namespace="$namespace" --set worker.enabled=false >"$directory/$version-render.yaml"
    done
    ruby -ryaml - "$directory" "$mode" "$image" <<'RUBY'
directory, mode, image = ARGV
names = [["ServiceAccount", "coyote-migrate"], ["SecretProviderClass", "coyote-migrate-secrets"]]
sentinel = {"apiVersion" => "v1", "kind" => "ConfigMap", "metadata" => {"name" => "ordinary-sentinel"}, "data" => {"revision" => "{{ .Release.Revision }}"}}
%w[old new bridge].each do |version|
  File.write("#{directory}/#{version}/Chart.yaml", {"apiVersion" => "v2", "name" => "transition-fixture", "version" => "0.1.0"}.to_yaml)
  documents = version == "bridge" ? [] : YAML.load_stream(File.read("#{directory}/#{version}-render.yaml")).compact.select { |document| names.include?([document["kind"], document.dig("metadata", "name")]) }
  abort "missing rendered prerequisites" unless version == "bridge" || documents.length == (mode == "csi" ? 2 : 1)
  documents.each do |document|
    annotations = document.dig("metadata", "annotations") || {}
    abort "old prerequisite must be ordinary" if version == "old" && annotations["helm.sh/hook"]
    abort "new prerequisite must be a hook" if version == "new" && annotations["helm.sh/hook"] != "pre-install,pre-upgrade"
  end
  if version == "new"
    job = YAML.load_stream(File.read("#{directory}/new-render.yaml")).compact.find { |document| document["kind"] == "Job" }
    abort "missing rendered hook Job" unless job
    job["metadata"]["name"] = "coyote-migrate-transition-{{ .Release.Revision }}"
    job["spec"]["activeDeadlineSeconds"] = 60
    pod = job.fetch("spec").fetch("template").fetch("spec")
    pod.delete("initContainers")
    pod["automountServiceAccountToken"] = false
    pod["volumes"] = [{"name" => "database-url", "secret" => {"secretName" => "coyote-database", "items" => [{"key" => "url", "path" => "database-url"}]}}]
    container = pod.fetch("containers").first
    container["image"] = image
    container["command"] = ["/bin/sh", "-ec"]
    container["args"] = ["test -s /var/run/secrets/coyote/database-url; exit {{ default 0 .Values.testExitCode }}"]
    container.delete("env")
    documents << job
  end
  documents << sentinel
  File.write("#{directory}/#{version}/templates/resources.yaml", documents.map(&:to_yaml).join)
end
RUBY
    args=(transition --namespace "$namespace" --wait --timeout 120s)
    resources=(serviceaccount/coyote-migrate)
    [[ "$mode" != csi ]] || resources+=(secretproviderclass/coyote-migrate-secrets)
    expected_count="${#resources[@]}"
    helm install "${args[@]}" "$directory/old"
    helm get manifest transition -n "$namespace" >"$directory/ordinary-manifest.yaml"
    kubectl -n "$namespace" get "${resources[@]}" -o json >"$directory/ordinary-resources.json"
    jq -e 'if .kind == "List" then .items else [.] end | all(.[]; .metadata.annotations["meta.helm.sh/release-name"] == "transition" and .metadata.labels["app.kubernetes.io/managed-by"] == "Helm" and .metadata.annotations["helm.sh/hook"] == null)' "$directory/ordinary-resources.json" >/dev/null
    sentinel_uid="$(kubectl -n "$namespace" get configmap ordinary-sentinel -o jsonpath='{.metadata.uid}')"
    if [[ "$scenario" == bridge-* ]]; then
      helm upgrade "${args[@]}" "$directory/bridge" >"$directory/bridge.log" 2>&1
      helm get manifest transition -n "$namespace" >"$directory/bridge-manifest.yaml"
      ruby -ryaml -e 'documents = YAML.load_stream(File.read(ARGV[0])).compact; abort "bridge removed unrelated resources" unless documents.map { |document| document.dig("metadata", "name") } == ["ordinary-sentinel"]' "$directory/bridge-manifest.yaml"
      assert_prerequisites 0 after-bridge
    fi
    before_revision="$(kubectl -n "$namespace" get configmap ordinary-sentinel -o jsonpath='{.data.revision}')"
    if [[ "$scenario" == *failure ]]; then
      if helm upgrade "${args[@]}" "$directory/new" --set testExitCode=1 >"$directory/first-failure.log" 2>&1; then
        echo "Expected failed hook" >&2; exit 1
      fi
      assert_hooks Failed
      assert_prerequisites "$expected_count" after-failure
      [[ "$(kubectl -n "$namespace" get configmap ordinary-sentinel -o jsonpath='{.data.revision}')" == "$before_revision" ]] || exit 1
    fi
    helm upgrade "${args[@]}" "$directory/new" --set testExitCode=0 >"$directory/first-success.log" 2>&1
    assert_hooks Succeeded
    transition_count=0
    [[ "$scenario" != bridge-* ]] || transition_count="$expected_count"
    assert_prerequisites "$transition_count" after-transition
    echo "$mode/$scenario: prerequisites remaining after first successful transition: $transition_count" | tee "$directory/result.txt"
    helm upgrade "${args[@]}" "$directory/new" --set testExitCode=0 >"$directory/second-success.log" 2>&1
    assert_hooks Succeeded
    assert_prerequisites "$expected_count" after-second
    jq -e 'if .kind == "List" then .items else [.] end | all(.[]; .metadata.annotations["helm.sh/hook"] == "pre-install,pre-upgrade" and .metadata.annotations["helm.sh/hook-delete-policy"] == "before-hook-creation")' "$directory/after-second.json" >/dev/null
    helm get manifest transition -n "$namespace" >"$directory/hook-release-manifest.yaml"
    ruby -ryaml -e 'abort "hook prerequisites remain ordinary" if YAML.load_stream(File.read(ARGV[0])).compact.any? { |document| ["ServiceAccount", "SecretProviderClass"].include?(document["kind"]) }' "$directory/hook-release-manifest.yaml"
    [[ "$(kubectl -n "$namespace" get configmap ordinary-sentinel -o jsonpath='{.metadata.uid}')" == "$sentinel_uid" ]] || exit 1
    helm uninstall transition -n "$namespace"
    kubectl delete namespace "$namespace" --wait=true
  done
done
echo "Helm transition verified: direct upgrade unsafe; maintenance bridge, subsequent upgrade and failure/retry passed in native and CSI lifecycle fixtures"