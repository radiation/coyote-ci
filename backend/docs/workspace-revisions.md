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