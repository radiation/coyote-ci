# Artifact Model

This repository currently uses four related artifact concepts:

- Artifact declaration: a pipeline config declaration that says a build step may publish a path.
- Artifact instance: one produced blob/row from one build. In code this remains `BuildArtifact` to avoid churn.
- Artifact package: the first-class repository identity for build artifacts. It is scoped by `job_id + logical_path` when a job exists, otherwise `build_id + logical_path`.
- Artifact version: an immutable version assignment for one artifact package that points at one concrete artifact instance.
- Artifact channel: a mutable alias such as `latest` or `prod` that points at the current concrete artifact instance for one package.
- VersionTag: the compatibility API read model returned by existing version-tag endpoints. Artifact-backed responses are assembled from artifact versions and artifact channels; managed image version tags remain backed by the legacy `version_tags` table.
- Collected artifacts with a matching declaration inherit their build's resolved application version as an immutable artifact version by default. An artifact-local version template takes precedence; `version.inherit_application_version: false` opts out. Inherited versions do not create or move channels.

Two intentional constraints for the current design:

- Artifact packages now have first-class storage so immutable versions and mutable channels can be modeled separately without overloading target-level tags.

- Both worker collection and authorized Kubernetes helper uploads commit artifact metadata and pending trigger deliveries in one transaction. Blob upload alone is not publication. A metadata failure can leave unreferenced bytes in the store.
- Enabled consumers in the same project match an exact logical path and producer job at publication. Self triggers and artifact-trigger chains are excluded. Later trigger edits affect future publications, not committed deliveries. Consumers use their current pipeline when processed.
- The server scans pending atomic deliveries at startup and every ten seconds, up to 64 per page in delivery-ID order. Success, failure, and cancellation all release the producer gate. Definite failures retain operator retry; ambiguous legacy deliveries are not replayed. No historical scan or backfill runs.
- Each delivery processing attempt has a two-minute deadline. Interrupted attempts do not mark pending deliveries failed; recovery can try them again without operator intervention.
- Rollout: drain and stop all old workers and servers before enabling new writers. Apply through migration 61, then start the updated servers and workers. Do not run mixed versions: old publishers can still commit metadata without obligations.
- Mutable aliases such as `latest`, `stable`, or `prod` are modeled as artifact channels with movement history rather than mutable version tags.