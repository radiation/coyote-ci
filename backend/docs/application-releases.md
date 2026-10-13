# Generic application releases

`pipeline.release` is opt-in coordination for immutable application outputs. It
is separate from artifact-local version channels and Coyote product releases.

```yaml
pipeline:
  application_version:
    template: "1.4.{build_number}"
  release:
    artifacts: [api-dist]
    images: [build-api]
    auto_channel: latest

artifacts:
  - name: api-dist
    path: dist/api.tar.gz

steps:
  - name: build-api
    image_build:
      context: .
      dockerfile: Dockerfile
      image: example/app/api
```

Release artifact entries reference named, exact-path artifact declarations.
Image entries reference `image_build` step names and their provider-relative
logical image path. The expected output set is
snapshotted after the Build application version resolves and before execution.

A release is `pending`, `complete`, or `failed`. The control plane marks it
complete only when every expected artifact/version or image digest is durable.
If configured, `latest` moves in that same database transaction and receives a
channel-history event. `stable` is never automatic; operators promote a
complete release through `POST /api/application-releases/{releaseID}/channels/stable`
or `coyote release promote <releaseID> --channel stable`.

Successful immutable artifacts and image digests remain available if another
output fails. Server-side recovery continuously reconciles pending releases
from the V2A artifact and V2B image publication records.

Jobless Builds may create coordinated releases without channels. Generic
`latest` and `stable` require a job-backed application stream: jobless Builds
must not configure `auto_channel`, and cannot be manually promoted.

## Coyote dogfood release bridge

Coyote's own pipeline declares four image outputs—server, frontend, worker,
and migrate—and uses generic release completion only as an immutable output
gate. It does not configure generic `latest` or `stable`.

After a generic release is complete, a trusted release environment can publish
the separate Coyote product manifest:

```sh
coyote release publish-coyote --build <build-id> \
  --release-source <release-store> --channel latest
```

This command validates the four digest-pinned generic outputs and reuses the
existing Coyote product manifest and channel system. Generic completion does
not imply Coyote product `latest` has advanced.
