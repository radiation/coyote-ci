# Coyote CI Artifact Blobs on GCS

This document describes a GCP deployment profile for storing Coyote artifact blobs in Google Cloud Storage (GCS).

Product-level model:

- Artifact metadata is persisted in PostgreSQL.
- Artifact blobs are persisted in the configured artifact store.
- Coyote core logic remains provider-agnostic; GCS is one deployment profile.

## When to use GCS

Use GCS for production and multi-node deployments where local filesystem storage is not shared between nodes.

Filesystem storage remains appropriate for local development and simple single-node installs.

## Required runtime configuration

Set these for both backend and worker:

```bash
ARTIFACT_STORAGE_PROVIDER=gcs
ARTIFACT_GCS_BUCKET=coyote-artifacts-prod
ARTIFACT_GCS_PREFIX=coyote-ci
ARTIFACT_STORAGE_STRICT=true
```

Optional for local/simple installs:

```bash
# default local filesystem mode
ARTIFACT_STORAGE_PROVIDER=filesystem
ARTIFACT_STORAGE_ROOT=/var/lib/coyote-artifacts
```

## Bucket and key conventions

- Bucket should be private (no public read).
- Prefix is optional but recommended (for tenancy/environment partitioning).
- Coyote writes provider-native object keys under the configured prefix.
- Persisted metadata keeps `storage_provider` and `storage_key` needed for retrieval.

Example object key shape:

- `coyote-ci/builds/{buildID}/steps/{stepID}/{artifactID}-{basename}`
- `coyote-ci/builds/{buildID}/shared/{artifactID}-{basename}`

## Credentials and access

Use one of:

- Workload Identity (recommended on GCP)
- Application Default Credentials in runtime environment
- Service account key injection via deployment secrets (last resort)

Required IAM scope is minimal object read/write access for the artifact bucket.

## Runtime behavior and failure mode

- With `ARTIFACT_STORAGE_STRICT=true`, startup fails if the GCS client/store cannot initialize.
- With `ARTIFACT_STORAGE_STRICT=false`, runtime falls back to filesystem when GCS setup fails.
- For production, prefer `ARTIFACT_STORAGE_STRICT=true` to avoid accidental mixed storage behavior.

## Direct object-store access future path

Current API download behavior still proxies reads through Coyote handlers.

The storage abstraction and metadata model already preserve provider + native object key, so future signed URL or redirect-based delivery can be added without changing artifact metadata contracts.
