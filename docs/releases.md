# Coyote software releases

A Coyote release is an immutable semantic version manifest containing digest-pinned
server, frontend, worker, and migration images in `registry/repository@sha256:<digest>`
form. Registries may retain semantic-version tags for discovery, but persisted manifests
never include those tags. The coordinated builder publishes temporary build tags,
commits the manifest, then promotes semantic registry tags to the committed digests.
`latest` and `stable` are mutable
channel pointers; moving one never alters an existing manifest.

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

## Publishing from a Coyote build

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
