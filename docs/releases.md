# Coyote software releases

A Coyote release is an immutable semantic version manifest containing digest-pinned
server, frontend, worker, and migration images in `registry/repository@sha256:<digest>`
form. Registries may retain semantic-version tags for discovery, but persisted manifests
never include those tags. `latest` and `stable` are mutable
channel pointers; moving one never alters an existing manifest.

The currently supported provider-neutral release source is a filesystem directory
or `file://` URL with `releases/<version>.json` and `channels.json`. It does not
require a running Coyote server. OCI and mirrored HTTP sources can implement the
same release-source interface later.

Resolve an overlay before Helm deployment:

```sh
coyote release resolve --release 2.5.1 --release-source ./release-store \
  --output resolved-release-values.yaml
```

Pass the generated file after the operator values file. Kubernetes then receives
only immutable image digest references.
