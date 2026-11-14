# Release Workflow Retirement And Legacy Recovery

New releases use the [five-component V3 pipeline and Go publisher](releases.md).
There is no release-specific plan, predecessor selection, reuse classifier,
approval document, or separate assembly workflow.

## Before And After

| Area | Before | Current path |
| --- | --- | --- |
| Images | Three dogfood images; separate five-image predecessor builder | All five images in `.coyote/pipeline.yml`, both Linux architectures |
| Manifest | Four-image Go V1 bridge or shell-generated V2 | Existing Go bridge produces complete V3 from one completed Build |
| Helm | Independent shell publisher and legacy selections | One unified chart packaged by the pipeline, verified/published by Go CLI |
| CLI | Independent snapshot matrix | Existing GoReleaser packages six archives with checksums/source metadata |
| Channels | Multiple publishing paths/default promotion | Existing store-backed operations; no product-channel promotion by default |

Deleted superseded files: `scripts/release-build.sh`,
`scripts/test-release-build.sh`, `scripts/test-coyote-release-plan.sh`,
`scripts/test-coyote-release-publish.sh`, `scripts/helm-publish-oci.sh`, and
`scripts/test-helm-publish-oci.sh`. Their Make targets and caller references are
removed; `release-publish-test` now runs the Go release/CLI tests. The GCS
release-store live test retains its explicit opt-in and recovery-store checks,
but no longer invokes predecessor planning.

The standalone Backend Build Server, Backend Build Worker and Frontend Build
pipeline steps are also removed: their compilation is performed by the source
image stages after the existing format/vet/test dependencies pass. Package steps
declare their own outputs for worker collection; matching top-level declarations
provide the generic release's immutable output identities.

## Retained Recovery Files

| Files | Why retained | Removal trigger |
| --- | --- | --- |
| `scripts/coyote-release`, `scripts/test-coyote-release-deploy.sh`, `scripts/test-coyote-release-promote.sh`, `deploy/release-targets/` | Existing target-aware V1/V2 assessment/promotion users; plan/publish code has been removed | Successful unified-chart cutover and expiration of legacy recovery needs; remove these files and their Make targets |
| `deploy/helm/coyote-ci-worker/`, legacy-worker options in `scripts/helm-package-validate.sh` and `scripts/helm-migration-parity.sh` | Historical two-release GKE installation and package/render recovery | Same cutover/recovery sign-off; remove worker chart, legacy options and corresponding test fixtures |
| `scripts/gke-helm-rollout.sh`, `scripts/gke-helm-adopt.sh`, `scripts/gke-helm-normalize-ownership.sh`, related `scripts/test-gke-helm-*.sh` | Still-running installation and its documented recovery procedure; no runtime behavior changed | Explicit retirement of the old installation/recovery procedure; remove these scripts, tests and Make targets together |
| V1/V2 manifest readers, import/fetch/resolve and stored release data | Historical immutable releases must remain readable byte-for-byte | Only after a separately approved archival/retention policy removes all consumers; not part of this cutover |

The legacy assessor compares against its supported historical chart revision.
Its hard-coded 0.1.0 test fixture is not a certification of the current unified
0.2.0 chart. Do not infer a clean V3 deployment from that legacy assessment.

## Channel Promotion

Promote an already published immutable release as a separate operation:

```sh
scripts/coyote-release promote \
  --release 0.0.11 \
  --channel latest \
  --target production
```

`promote` accepts only exact semantic versions and the `latest` or `stable`
channels. It validates the target profile, fetches and checks the existing
manifest before mutation, then calls the release CLI's existing store-backed
channel promotion operation. The result reports the previous channel target,
new target, manifest digest, and whether a channel-index write occurred.
Repeating a promotion of the same release is idempotent. Filesystem and
successful GCS CAS operations report their exact prior entry and change result.
After an ambiguous GCS transport failure, when the store can only confirm that
the requested entry is now present, the prior target and `Changed` are reported
as `<unknown>` rather than inferred. Filesystem and GCS stores retain their
existing locking and generation-based CAS behavior.
Promotion does not build images, publish manifests, or deploy workloads.

Run the focused promotion checks with:

```sh
make release-promote-test
```

## Deployment Assessment

Assess a published V2 release against a selected Kubernetes target without
changing that target:

```sh
scripts/coyote-release deploy \
  --release 0.0.10 \
  --target production
```

`deploy` requires a non-secret `kubernetes` object in the target profile:

```json
{
  "kubernetes": {
    "context": "production-context",
    "cluster_uid": "kube-system-namespace-uid",
    "control_plane_release": "coyote-ci",
    "worker_release": "coyote-ci-worker"
  }
}
```

`worker_release` is optional and is only used with `--scope worker`; the
default `control-plane` scope never queries or operates on a worker release.
The assessor validates the active context and `kube-system` namespace UID
before it fetches the immutable manifest from the profile's release source.

It renders a temporary manifest-derived Helm overlay, reads Helm values,
status, and the installed Helm manifest, and compares immutable image digests
for server, frontend, and a Helm-managed MCP deployment. Image references compare by digest, so
`tag@sha256:...` and digest-only references are equivalent. Migration images
are checked through the manifest and render input; historical migration Jobs
are not runtime workloads.

The result is one of:

- `runtime-equivalent`: workload topology and immutable runtime digests match,
  while supported Helm release metadata or values do not.
- `release-equivalent`: runtime equivalence plus matching supported chart
  identity, release version and manifest-digest values, effective image inputs,
  and a canonical comparison between the temporary rendered manifest and the
  installed Helm manifest. If installed-manifest evidence is unavailable, the
  assessor reports only `runtime-equivalent`.
- `drift`: a workload, Helm-managed MCP topology, or required Helm input differs.
- `migration-reconciliation-required`: a historical migration Job has no
  unique completed matching local rollout checkpoint, or its checkpoint does
  not match the target context, cluster UID, namespace, control release,
  rollout ID, migration image, and completed migration phase.

The command only calls release fetch, Helm read/template operations, and
Kubernetes read operations. It never runs Helm upgrade/install, creates or
deletes migration Jobs, writes checkpoints, scales deployments, promotes a
channel, or publishes a release. It intentionally does not suggest an apply
operation when target identity or migration lineage is unresolved.

Run the focused assessment checks with:

```sh
make release-deploy-test
```