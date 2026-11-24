# Coyote software releases

A Coyote application release has one immutable semantic version. Initially every
release builds all five images: `server`, `frontend`, `worker`, `mcp`, and
`migrate`. Each newly built component uses that application release version as
its component version. Images are recorded as immutable
`registry/repository@sha256:<digest>` references, not mutable tags.

The initial container platforms are `linux/amd64` and `linux/arm64`, configured
in the image-building system. Architectures do not require a separate release
assembly mechanism. Selective rebuilding and artifact reuse selection are
deferred to generic Coyote pipeline features, not a release-specific planner,
change classifier, predecessor resolver, assembly document, or `assemble` command.

`latest` and `stable` are mutable channel pointers; moving one never alters an
existing manifest. The pipeline and existing Go publisher now produce complete V3 releases.

## One Build, One Publication

Run the repository pipeline from a pinned source commit with its Build-owned
`COYOTE_APPLICATION_VERSION`. It builds all five images for `linux/amd64` and
`linux/arm64` through Cloud Build's standard Docker Buildx path. No predecessor,
selective rebuild, reused image, or separate assembly workflow is involved.
GoReleaser runs from the repository root with its build directory set to the
`backend` module; no local `go.work` is required. It packages six CLI archives (Darwin, Linux, Windows; amd64 and arm64),
checksums and source/version metadata. Helm packages only the unified chart,
stamping both `version` and `appVersion` with the application version.

Go image builders run the compiler on Buildx's native `BUILDPLATFORM` and
cross-compile for `TARGETOS`/`TARGETARCH`. Frontend dependency installation and
static compilation also run on `BUILDPLATFORM`: Rolldown, Lightning CSS, and
TypeScript's native tooling are build-time dependencies, not runtime binaries.
Only `dist` is copied into target-platform Nginx; the frontend Docker context
excludes host dependencies and generated output.

Backend CA bundle generation runs in a native Debian stage. Only its generated,
architecture-independent certificate bundle crosses into the runtime. Git is
still required for source operations; Git and its libraries, MCP's curl, and
runtime account setup remain target-platform operations and may need emulation.
Cloud Build uses the unchanged pipeline step timeouts; measure an explicitly
approved cold build before changing deadlines or machine sizes. Local compilation
does not establish a Cloud Build time budget.

The CLI packaging step downloads GoReleaser v2.12.7's official Linux binary for
the execution host, verifies its checked-in SHA-256 before extraction, and removes
temporary tool files on exit. Updating that version requires updating both host
architecture checksums. This avoids compiling GoReleaser's own large dependency
graph inside the execution Pod. `--parallelism 1` bounds simultaneous cross-build
temporary files. Only this step disables shared cache restore/save; the Go cache
mounts remain available for reuse within the six-target run. Other pipeline cache
policies, output declarations, version stamping, and publication checks are unchanged.

No Kubernetes resource requests or limits are raised. The current pipeline schema
has no per-step resource override. A cold local packaging run with 2 CPUs and
4 GiB memory produced all six archives in 2m37s, with approximately 3.9 GiB of final
workspace/module/build-cache data. This is not a live Pod peak measurement: helper
volumes, temporary compilation files, and workspace publication overhead still
need headroom within the admitted Pod storage budget.

After that build and its generic application release are complete, use the
existing CLI from an authorized publication environment:

```sh
coyote release publish-coyote --build BUILD_ID \
  --release-source ./release-store \
  --helm-repository oci://REGISTRY/charts/coyote-ci \
  --immutable-chart-tags-confirmed
```

This command does not promote a channel by default. `--channel latest` or
`--channel stable` explicitly opts into the existing post-publication channel
operation; no promotion is needed to publish a manifest.

Publication guarantees:

- Versioned image submission requires verified Artifact Registry immutable-tag
  enforcement. Successful multi-platform builds are resolved from the registry
  tag, checked for OCI-index/manifest-list media type and both architecture
  children, and persisted with the index digest, not a child image digest.
- The publisher requires a successful Build, matching completed generic release,
  all five published image outputs, and all eleven declared package outputs.
  Archive bytes must match the artifact API size/SHA-256 and GoReleaser checksums;
  CLI metadata must match the Build's version and source commit.
- All five component versions equal the application version. Assembly, component
  and CLI provenance identify that actual Coyote Build and its pinned source.
  External Cloud Build execution IDs remain in the image-build records.
- Compatibility comes from `release/compatibility.json`. Its canonical Goose
  target must match the source-produced target and `target` run against both
  platforms of the exact digest-pinned migration image. These checks use no
  database credentials, no container network, and a read-only root filesystem.
- Chart name/version/appVersion are verified before publication. Helm pulls the
  published chart and compares its bytes with the build artifact. Docker Buildx
  resolves its OCI descriptor as JSON; Helm then pulls by that digest and the
  bytes are verified again before the digest is recorded. No Helm log parsing is
  required. An identical existing chart is retryable; different bytes conflict.
  Only `manifest unknown` or Helm 4's exact `<reference>: not found` response
  permits a push; authentication and ambiguous errors fail.
- The release manifest is written only after these checks. Existing conditional
  immutable-store writes, identical retry behavior, and conflicting-byte rejection
  remain intact. Channels are written only after the manifest succeeds. A failed
  channel write can be retried without rebuilding or replacing the manifest.

Image/chart uploads are not a cross-registry transaction: a failed publication
can leave verified images or a chart without a product manifest. It cannot create
a partial product manifest or advance a product channel before that manifest.
Retry the same completed Build, not a different Build with the same immutable tag.

### Publication Prerequisites

The running control plane must include this platform-aware image adapter and
pipeline contract. Cloud Build needs Docker daemon access, Buildx, privileged
binfmt registration for both architectures, shared Docker authentication, source
archive access and Artifact Registry writer/reader IAM. These assumptions require
an approved real-registry end-to-end run; local tests use fake cloud responses.

The publisher needs a Coyote API context/token with build and artifact read
access, release-store access, authenticated Helm and Docker with Buildx, and a
Docker host able to execute both Linux architectures. Helm and Docker must both
have pull access to the chart registry; Helm-only login is insufficient for the
descriptor lookup. Chart tag immutability and retention
are operator-enforced prerequisites, explicitly acknowledged by the flag; the
generic Helm path does not change or inspect registry policy. Keep artifacts,
images, chart versions and API download URLs available for the release lifetime.
CLI URLs currently use authenticated Coyote artifact-download endpoints, not a
new public download service.

`release/compatibility.json` currently declares Goose target 58 and the existing
0.0.11 maintenance-cutover baseline (`>=0.0.11 <0.0.12`). Review that declaration
for each supported upgrade; it does not certify online-safe DDL or authorize
deployment. No infrastructure changes, production publication or promotion are
part of local validation.

## Recovering the existing 0.0.15 Build

Build #13 (`1ce1f16d-ccb8-467c-81ab-eb16a5bd21d4`) succeeded at source
`9494d7c1cfa36f6f17735c6d33ad13b1554c8448`. Its generic release is
`ff89ef4a-c0e9-4ce7-aa92-138b85b69e7c`, version `0.0.15`, with eleven
artifact outputs and five image outputs. Do not rebuild, rerun, or reuse this
version for a different Build.

The reconciliation fix closes and checks pgx query rows before reusing a
transaction. Artifact outputs may be build-scoped or step-scoped, but must match
exactly one artifact in the same Build with the expected name/path and a version
owned by that artifact's package. Missing versions leave outputs pending;
ambiguous candidates fail reconciliation without partially committing changes.

Kubernetes terminal completion now invokes the existing declaration-based
artifact versioning lifecycle through the build service. The existing server
pending-release loop also retries version creation for successful Builds using
their persisted pipeline snapshot. Recovery and duplicate terminal reports
create immutable versions only; they do not replay artifact channel movements.
Only the step completion transaction that changes the build to success may
honor explicitly configured artifact channels; delayed callbacks for other steps
remain version-only. Generic `latest` remains opt-in and transactional; `stable` and Coyote
product publication remain separate explicit operations.

Recovery procedure (not performed as part of this fix):

1. Before authorizing any rollout, use an authenticated read-only API context to
  inspect `GET /api/builds/1ce1f16d-ccb8-467c-81ab-eb16a5bd21d4`,
  `GET /api/builds/1ce1f16d-ccb8-467c-81ab-eb16a5bd21d4/artifacts`, and
  `GET /api/builds/1ce1f16d-ccb8-467c-81ab-eb16a5bd21d4/application-release`.
  Confirm success, the source/version above, the same release ID, all sixteen
  expected output definitions, and no automatic generic channel. Re-download
  the eleven artifacts and verify size/SHA-256 against persisted metadata and
  the investigation evidence. Recheck the five registry publications against
  the recorded immutable digest references. Stop on any mismatch.
2. Inspect `GET /api/artifacts/{artifactID}/version-tags` for each of those eleven
  IDs and `GET /api/jobs/{jobID}/version-tags?version=0.0.15` using the Build's
  job ID. Existing versions are acceptable only when they belong to the exact
  expected artifact/package; never delete, reassign, or overwrite a conflict.
  Record existing artifact, generic application, and product channel pointers.
3. In a separately approved operational change, make the fixed control plane
  available. **Its existing recovery loop runs immediately at startup and then
  every ten seconds for pending releases**, not only Build #13. Enabling it
  authorizes version backfill and generic reconciliation; it is not a read-only
  rollout. No manual database edits, new Build, repair job, or recovery endpoint
  are needed. A version conflict or database failure keeps the release pending
  and is logged; correct the underlying issue and allow the same loop to retry.
4. Read the same Build/release endpoints again. Require the original Build to
  remain successful, the same release to become `complete`, and all sixteen
  outputs to be `published`. Each artifact output must point to its original
  artifact and a `0.0.15` immutable version; each image must retain its verified
  digest reference. Repeat the version-tag reads to confirm exactly those
  eleven version identities. Confirm all channel pointers remain unchanged.
5. Stop after generic release recovery. This does **not** publish Helm OCI
  `0.0.15`, create the Coyote product manifest, promote a product channel, or
  deploy the recovered release. Those remain separate approvals and operations.

The durable catch-up scan is limited to pending generic releases. Builds without
`pipeline.release` use terminal versioning but are not covered by that scan.
Artifact channel failures are not replayed by recovery. No schema change or new
background service is introduced.

## V3 application release manifest

Schema version 3 is an immutable application-release contract. `release` is the
public application version and is the release-store identity; the manifest does
not add a second release identifier. `assembly` identifies the build that
assembled that application release and the source revision it used. It is
separate from each component's original `build` provenance, so independently
built or reused components retain their own immutable identity and source.
This provenance field is not a separate assembly document or workflow.
The contract does not require a predecessor manifest or Git ancestry between
assembly and component source revisions. Its ability to represent independently
built components does not change the initial all-component build policy.

Every V3 manifest contains exactly the `server`, `frontend`, `worker`, `mcp`,
and `migrate` components. Each supplies an exact component version, a
digest-pinned image reference, and its original build id and source metadata.
V3 also contains one `helm` package named `coyote-ci`; its OCI repository and
digest are immutable and its version must equal `release`.

`cli_artifacts` is keyed by `os-arch` (for example `linux-amd64`). Every entry
contains the absolute HTTP(S) download `url`, archive `filename`, positive byte
`size`, lowercase hexadecimal `sha256` checksum of the distributed archive
bytes, and `build_id`. The manifest's `compatibility.schema_target` is the
nonzero decimal Goose migration version included in the digest-pinned `migrate`
component image (for example `"57"` for migration file `00057_*`); it declares
the target only, while Goose remains responsible for tracking and applying
migrations. New releases omit `compatibility.upgrade_from`. The updated CLI
still validates a legacy range and passes it to older digest-pinned charts.
Older CLIs require the range; update the publisher and resolver before using
a release without it. Do not rewrite an existing manifest or replace its chart.
`maintenance` is one of `active`, `maintenance`, or `end-of-life`. It is not a
downtime authorization flag.

V3 Helm image overlays carry `compatibility.schema_target`. The updated chart
accepts forward upgrades without a source-version range; downgrades remain
unsupported. This is not approval to apply pending migrations. The migration hook
checks the declared target against the bundled migration files before database
writes and checks Goose history under an advisory lock. Publishers must verify
the target against the exact image digest (`docker run --rm "$COYOTE_MIGRATE_IMAGE"
target`); runtime verification does not replace publication validation or SQL
immutability. A forward version number does not certify online-safe DDL.
Online and maintenance approvals, missing source history, timeout/retry behavior,
and the application-only rollback boundary are documented in the
[migration runbook](../deploy/docs/database-migrations.md).

```json
{
  "schema_version": 3,
  "release": "3.0.0",
  "assembly": {
    "build_id": "assembly-build-300",
    "source": {"commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "created_at": "2026-11-13T12:00:00Z"}
  },
  "components": {
    "server": {"version": "3.0.0", "image": "registry.example/coyote-server@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "build": {"id": "server-build-300", "source": {"commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "created_at": "2026-11-13T12:00:00Z"}}},
    "frontend": {"version": "3.0.0", "image": "registry.example/coyote-frontend@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "build": {"id": "frontend-build-300", "source": {"commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "created_at": "2026-11-13T12:00:00Z"}}},
    "worker": {"version": "3.0.0", "image": "registry.example/coyote-worker@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", "build": {"id": "worker-build-300", "source": {"commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "created_at": "2026-11-13T12:00:00Z"}}},
    "mcp": {"version": "3.0.0", "image": "registry.example/coyote-mcp@sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", "build": {"id": "mcp-build-300", "source": {"commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "created_at": "2026-11-13T12:00:00Z"}}},
    "migrate": {"version": "3.0.0", "image": "registry.example/coyote-migrate@sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "build": {"id": "migrate-build-300", "source": {"commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "created_at": "2026-11-13T12:00:00Z"}}}
  },
  "helm": {"name": "coyote-ci", "repository": "oci://registry.example/charts", "version": "3.0.0", "digest": "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
  "cli_artifacts": {"linux-amd64": {"url": "https://downloads.example/coyote_3.0.0_linux_amd64.tar.gz", "filename": "coyote_3.0.0_linux_amd64.tar.gz", "size": 12345, "sha256": "1111111111111111111111111111111111111111111111111111111111111111", "build_id": "cli-build-300"}},
  "compatibility": {"schema_target": "57", "maintenance": "active"}
}
```

## Legacy V1/V2 publication and recovery

The predecessor-based shell builder/planner and standalone Helm publisher have
been removed. See the [retirement inventory](release-planning.md) for retained
legacy recovery tools and their removal conditions.

V1 and V2 manifests remain accepted as stored. Fetch and import preserve their
original bytes and digest; resolution does not synthesize V3 assembly,
component-build, Helm, CLI, or compatibility metadata. Channels remain in the
separate mutable `channels.json` index for all manifest versions.
Legacy readers continue to ignore V3-named extensions, including non-object
values and V2 component `build` fields; these extensions are not interpreted as
V3 metadata. V3 readers retain strict field validation.

The currently supported provider-neutral release source is a filesystem directory,
`file://` URL, or `gs://bucket[/prefix]` location. Each source stores
`releases/<version>.json` and `channels.json`; Google Cloud Storage uses
Application Default Credentials or workload identity. It does not require a
running Coyote server. OCI and mirrored HTTP sources can implement the same
release-source interface later.

GCS release publishers require `storage.objects.get`,
`storage.objects.create`, and `storage.objects.delete` on the release prefix.
Cloud Storage requires create and delete permission to replace the mutable
`channels.json` object, even when the replacement uses a generation-match
precondition. Immutable release manifests are still created only with a
does-not-exist precondition.

`coyote release fetch` exits with status `3` only when the requested manifest does
not exist. Status `1` covers storage, authentication, permission, and output
failures; status `2` indicates invalid command input. Fetch returns stored bytes
without parsing them, so commands that consume the manifest validate malformed
data. Release automation may treat status `3` as an absent target release, but
must treat every other nonzero status as a failure.

Resolve an overlay before Helm deployment:

```sh
coyote release resolve --release 2.5.1 --release-source ./release-store \
  --output resolved-release-values.yaml
```

Pass the generated file after the operator values file. Kubernetes then receives
only immutable image digest references.

## Local Release Checks

```sh
GOWORK=off COYOTE_APPLICATION_VERSION=0.0.41 make cli-snapshot cli-validate-release-matrix
make release-publish-test backend-architecture
make helm-package-validate
make pre-push-check
```

The Helm target runs the consolidated structural checks once and includes
package-integrity self-tests. It requires Helm and Ruby locally; Ruby is not
required by the Go release CLI or standard Helm deployment.
These checks do not publish to a registry or promote channels. Registry auth,
Cloud Build execution, index metadata visibility, immutable-tag conflicts and
Helm push/pull behavior still require an explicitly authorized integration run.

Before retrying a failed release, use a commit containing the fixes and check
version reservations and existing immutable outputs; a failed Build's captured
pipeline is not updated by editing the repository. Verify public API access and
load-balancer backend health, not just Pod readiness. For a public HTTP 503,
correlate Gateway/NEG events and load-balancer `statusDetails` with Pod eviction
and application logs. Node scale-down and backend convergence can interrupt a
single-replica API even after its replacement Pod becomes ready. Infrastructure
changes or workload restarts require separate approval.
