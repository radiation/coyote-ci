# Release Planning

`scripts/coyote-release plan` is a read-only facade for preparing a Coyote
product release. It reads Git history and an existing release store, then writes
a local plan. It does not build or push images, publish manifests, promote
channels, call Kubernetes, or modify deployment state.

Build the existing release CLI first:

```sh
make cli-build
```

Create an operator-owned, non-secret profile at
`.local/coyote/release-targets/<target>.json`. Start from
`deploy/release-targets/example.json`:

```json
{
  "schema_version": 1,
  "target": "production",
  "release_source": "gs://example-coyote-releases",
  "registry": "us-central1-docker.pkg.dev/example-project/coyote-ci"
}
```

Profiles contain only stable release-routing data. Do not put credentials,
tokens, secret values, or IAM configuration in them. GCS access continues to
use ADC or workload identity. A profile may use a filesystem path, `file://`
URL, or `gs://` release source.

Generate a plan:

```sh
scripts/coyote-release plan \
  --release 0.0.11 \
  --from latest \
  --target production \
  --output .local/coyote/release-plans/0.0.11.json
```

`--from` accepts an exact release, `latest`, or `stable`. Channel selection is
resolved with `coyote release resolve`; the planner then fetches the exact
manifest bytes with `coyote release fetch` and calculates their SHA-256 digest.
The predecessor manifest commit must exist locally and be an ancestor of HEAD.
The worktree must be clean.

## Classification Rules

The planner compares the verified predecessor commit to HEAD. It uses these
conservative input rules:

- `frontend`: `frontend/**`
- `server` and `worker`: `backend/cmd/server/**`, `backend/cmd/worker/**`,
  `backend/internal/**`, `backend/go.mod`, `backend/go.sum`, and
  `backend/Dockerfile`
- `mcp`: `backend/cmd/mcp/**`, `backend/internal/**`, `backend/go.mod`,
  `backend/go.sum`, and `backend/Dockerfile`
- `migrate`: `backend/db/migrations/**` and `backend/Dockerfile`

Server and worker are always coupled. A shared backend dependency change rebuilds
server, worker, and MCP. A changed Dockerfile rebuilds server, worker, MCP, and
migrate. Any changed path without a rule places every component in
`manual_review`; it never becomes a silent reuse decision.

Use `--force-build COMPONENT` or `--force-reuse COMPONENT` for an intentional
operator override. A server or worker override applies to both runtime
components, and conflicting runtime overrides are rejected. Use
`--component-version COMPONENT=SEMVER` to override the version for a built
component; otherwise builds use the requested product release version and reuse
inherits the predecessor V2 component version. A reused V2 component rejects a
different version override. For a V1 predecessor, `--force-reuse COMPONENT` plus
an explicit `--component-version COMPONENT=SEMVER` is the operator declaration
of the historical component version; without both, that component remains
`manual_review`. Because server and worker are coupled, V1 runtime reuse needs
the explicit version declaration for both components.

## Plan Format And Hash

The output includes the current source commit, selected predecessor release and
manifest digest, profile digest, release source, registry, target, fixed build
date (when supplied), changed paths, unknown paths, and an entry per component.
Each component records its action, reason, version, resolved reusable image when
available, predecessor image, original source classification, and any explicit
action override. A forced reuse keeps the original build/manual-review evidence
in `classification` and is marked in `override`, so a later publish command can
revalidate the operator's decision.

New builds intentionally have `image: null`: a planner must not invent an image
digest before a build exists. V1 manifests have no component-version provenance,
so default reuse becomes `manual_review`; unresolved versions remain `null` and
the plan records any known predecessor image without fabricating a version.

`plan_sha256` is `sha256:` plus the SHA-256 of the compact, recursively
key-sorted JSON document with `plan_sha256` removed. The plan contains no clock
value unless `--build-date` is supplied, so identical verified inputs produce
identical JSON and hashes. A future publish command can recompute this canonical
document and reject a changed source commit, predecessor manifest, profile, or
component decision.

Run the focused planner checks with:

```sh
make release-plan-test
```

## Approved Plan Publication

After an operator has inspected the plan, publish that exact plan by supplying
both the file and its displayed hash:

```sh
scripts/coyote-release publish \
  --plan .local/coyote/release-plans/0.0.11.json \
  --expected-plan-sha256 sha256:... \
  --profile .local/coyote/release-targets/production.json
```

`publish` first validates the complete plan shape, recomputes its canonical
hash, and requires it to equal the explicit operator value. It then recreates
the plan from the recorded selector, profile, fixed timestamp, overrides, and
component version declarations. The current worktree, HEAD, predecessor
manifest bytes, target profile, source classification, component provenance,
and resolved decisions must produce the same canonical plan. Any
`manual_review` decision or rebuilt component without a fixed `build_date`
rejects publication.

Only after that validation does the facade delegate to
`scripts/release-build.sh`. The existing publisher remains the sole authority
for image construction, immutable manifest publication, retries, and semantic
component tags. The facade always sets `RELEASE_CHANNEL` to an empty value, so
approved-plan publication never advances `latest` or `stable`; channel
promotion remains a separate operator action.

Revalidation and the delegated publisher run in isolated environments. All
release-control variables are set from the approved plan, including
`RELEASE_PLAN=false`, the source commit, build date, component actions,
versions, and empty component image overrides. The facade preserves only
operational settings needed to use
local Docker credentials and GCS ADC: `HOME`, `PATH`, optional temporary/XDG
directories, Docker connection/configuration settings, Google credential/config
settings, proxy settings, and TLS certificate locations. Stale `RELEASE_*`,
`COMPONENT_*`, `GIT_*`, and unrelated `COYOTE_*` shell variables cannot alter
the publication. The executable selected by `COYOTE_RELEASE_CLI` and the
executables available through `PATH` remain local operator trust boundaries.

Run the focused publication checks with:

```sh
make release-publish-test
```

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