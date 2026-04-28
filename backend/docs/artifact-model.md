# Artifact Model

This repository currently uses four related artifact concepts:

- Artifact declaration: a pipeline config declaration that says a build step may publish a path.
- Artifact instance: one produced blob/row from one build. In code this remains `BuildArtifact` to avoid churn.
- Artifact: the repository/browse identity users see in the artifact UI. It is synthesized today from `job_id + logical_path` when a job exists, otherwise `build_id + logical_path`.
- VersionTag: an immutable version assignment attached to a produced artifact instance or managed image version.

Two intentional constraints for the current design:

- There is no first-class artifact identity table yet. The browse identity is synthesized so the current DB shape stays stable.
- Mutable aliases such as `latest`, `stable`, or `prod` are a future concept separate from `VersionTag`. They should not be modeled as mutable version tags.