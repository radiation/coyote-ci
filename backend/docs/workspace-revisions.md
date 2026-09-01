# Workspace Revisions

`WorkspaceRevision` is control-plane metadata for an immutable workspace result.
`WorkspaceRevisionStore` is the data-plane contract that publishes, restores, and
deletes its durable bytes; it does not update revision repository state.

The first store implementation is filesystem-backed. It writes objects beneath
its configured root at the provider-neutral key
`workspace-revisions/<revision-id>.tar.gz`. The object is a streaming tar+gzip
bundle. `content_digest` is `sha256:<hex>` over the exact stored `.tar.gz` bytes,
and `size_bytes` is the stored object size.

Publication uses a temporary object followed by an atomic hard-link create, so an
existing object is never overwritten. Repeated publication with identical bytes
returns the existing metadata; different bytes return a conflict. Restore extracts
into a staging directory, verifies the stream digest, and only then exposes the
destination. The destination must not already exist.

Archives contain directories and regular files only, with portable permission
bits and executable bits preserved. Symlinks, hard links, devices, sockets, and
FIFOs are rejected. Archive names must be relative, slash-separated paths confined
under the restore root; absolute, traversal, and Windows-drive paths are rejected.

Archive ordering, timestamps (Unix epoch), ownership fields, and permission bits
are normalized for stable publication of unchanged source trees. No Coyote-owned
transient/cache directory is currently evidenced in the workspace layout, so this
store applies no exclusions. `.coyote/trigger-artifacts` remains included because
it is supplied workspace content.

## Runtime Restore

Docker workers continue to reuse an available build-scoped local workspace. When
that directory is absent for a linear predecessor input and
`COYOTE_WORKSPACE_REVISION_STORAGE_ROOT` is configured, the worker resolves the
published predecessor revision and restores it into the conventional build
workspace before cache restoration and command execution. Missing or corrupt
revisions stop the step with a `workspace` failure; commands do not run.

Portable local reuse records the last successfully committed node for the
current worker process. For ordinary linear predecessor inputs, a present
directory with unknown or mismatched lineage is rejected rather than overwritten
or silently treated as the requested predecessor revision.

Portable restoration for fan-out and fan-in remains unsupported because the
current host layout assigns one writable directory per build. Existing local
fan-out and fan-in compatibility behavior is preserved when that directory is
already available. A future portable implementation requires execution-scoped
workspace identities to isolate fan-out descendants safely.