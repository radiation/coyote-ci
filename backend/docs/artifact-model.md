# Artifact Model

This repository currently uses four related artifact concepts:

- Artifact declaration: a pipeline config declaration that says a build step may publish a path.
- Artifact instance: one produced blob/row from one build. In code this remains `BuildArtifact` to avoid churn.
- Artifact package: the first-class repository identity for build artifacts. It is scoped by `job_id + logical_path` when a job exists, otherwise `build_id + logical_path`.
- Artifact version: an immutable version assignment for one artifact package that points at one concrete artifact instance.
- Artifact channel: a mutable alias such as `latest` or `prod` that points at the current concrete artifact instance for one package.
- VersionTag: the compatibility API read model returned by existing version-tag endpoints. Artifact-backed responses are assembled from artifact versions and artifact channels; managed image version tags remain backed by the legacy `version_tags` table.

Two intentional constraints for the current design:

- Artifact packages now have first-class storage so immutable versions and mutable channels can be modeled separately without overloading target-level tags.
- Mutable aliases such as `latest`, `stable`, or `prod` are modeled as artifact channels with movement history rather than mutable version tags.