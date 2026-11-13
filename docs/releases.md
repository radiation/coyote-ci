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
existing manifest. The V3 contract below is not yet a complete V3 publisher.

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
migrations. `compatibility.upgrade_from` is a nonempty, ascending, half-open
semantic-version interval written exactly as `>=MIN_VERSION <MAX_VERSION`, with
one ASCII space between comparators; compatibility is declarative in V3.
`maintenance` is one of `active`, `maintenance`, or `end-of-life`. It is not a
downtime authorization flag.

V3 Helm image overlays carry `compatibility.schema_target` and `upgrade_from`
without changing manifest bytes or the V3 contract. The automatic migration hook
checks the declared target against the bundled migration files before database
writes and checks Goose history under an advisory lock. Publishers must verify
the target against the exact image digest (`docker run --rm "$COYOTE_MIGRATE_IMAGE"
target`); runtime verification does not replace publication validation or SQL
immutability. A supported source range alone does not certify online-safe DDL.
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
  "compatibility": {"schema_target": "57", "upgrade_from": ">=2.5.0 <4.0.0", "maintenance": "active"}
}
```

## Legacy V1/V2 publication and recovery

The existing shell-based `scripts/release-build.sh` path remains operational
until a complete V3 publisher replaces it. Its coordinated builder publishes
temporary build tags, commits the legacy manifest, then promotes semantic
registry tags to the committed digests. Its predecessor/retry behavior is
legacy recovery behavior, not a requirement of the V3 contract.

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

`scripts/release-build.sh` passes `RELEASE_SOURCE` unchanged to the Coyote CLI for
predecessor and retry manifest reads as well as publication, so the same source
location works for filesystem and GCS-backed release builds.

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

## Legacy publishing from a Coyote build

Coyote's dogfood pipeline uses `Build.ApplicationVersion` as its canonical
release version and completes a separate generic application release before a
product manifest is published. From a trusted environment with API-token and
release-source access:

```sh
coyote release publish-coyote --build <build-id> \
  --release-source ./release-store --channel latest
```

The command requires a complete generic release with the four Coyote
server/frontend/worker/migrate digest outputs. It publishes the existing
immutable product manifest first, then advances product `latest` only when the
candidate `0.0.<build_number>` release is not older than the current channel.
Product `stable` remains an operator promotion; generic application channels
are not used by the Coyote dogfood workflow.
