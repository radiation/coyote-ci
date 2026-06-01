import { describe, expect, it } from "vitest";

import type { Build, BuildStep } from "../types";
import {
  buildDuration,
  buildLabel,
  buildStepCounts,
  compactTriggerMetadata,
  jobLabel,
  operationalBuildTitle,
  projectLabel,
  shortSHA,
  triggerKind,
} from "./BuildDetailPage.helpers";

function build(overrides: Partial<Build> = {}): Build {
  return {
    id: "build-123456789",
    project_id: "project-1",
    priority: 0,
    status: "queued",
    created_at: "2026-03-24T00:00:00Z",
    queued_at: "2026-03-24T00:00:01Z",
    started_at: null,
    finished_at: null,
    current_step_index: 0,
    attempt_number: 1,
    error_message: null,
    ...overrides,
  };
}

function step(status: BuildStep["status"]): BuildStep {
  return {
    id: `step-${status}`,
    build_id: "build-1",
    step_index: 0,
    name: status,
    command: "echo ok",
    status,
    worker_id: null,
    started_at: null,
    finished_at: null,
    exit_code: null,
    stdout: null,
    stderr: null,
    error_message: null,
  };
}

describe("BuildDetailPage helpers", () => {
  it("formats compact labels and trigger metadata", () => {
    const value = build({
      build_number: 42,
      project_name: " Platform ",
      job_name: " release ",
      trigger_kind: " webhook ",
      scm_provider: "github",
      trigger_ref: "refs/heads/main",
      trigger_commit_sha: "abcdef1234567890",
      actor: "dev@example.com",
    });

    expect(shortSHA(" abcdef1234567890 ")).toBe("abcdef1");
    expect(shortSHA(" ")).toBe("—");
    expect(triggerKind(value)).toBe("webhook");
    expect(compactTriggerMetadata(value)).toBe(
      "github • refs/heads/main • abcdef1 • dev@example.com",
    );
    expect(buildLabel(value)).toBe("Build #42");
    expect(operationalBuildTitle(value)).toBe("release #42");
    expect(projectLabel(value)).toBe("Platform");
    expect(jobLabel(value)).toBe("release");
  });

  it("falls back when optional labels are absent", () => {
    const value = build({
      project_slug: " platform ",
      job_id: "job-123456789",
    });

    expect(triggerKind(build({ trigger_kind: " " }))).toBe("manual");
    expect(compactTriggerMetadata(build())).toBe("");
    expect(buildLabel(value)).toBe("Build build-12");
    expect(operationalBuildTitle(value)).toBe("Job job-1234 · build-12");
    expect(projectLabel(value)).toBe("platform");
    expect(jobLabel(value)).toBe("Job job-1234");
    expect(jobLabel(build())).toBe("Manual");
  });

  it("counts step states and formats durations", () => {
    expect(
      buildStepCounts([
        step("pending"),
        step("running"),
        step("success"),
        step("failed"),
        step("canceled"),
      ]),
    ).toEqual({
      total: 5,
      success: 1,
      failed: 1,
      canceled: 1,
      running: 1,
      pending: 1,
    });
    expect(buildStepCounts(undefined).total).toBe(0);

    expect(
      buildDuration(
        build({
          status: "success",
          started_at: "2026-03-24T00:00:00Z",
          finished_at: "2026-03-24T00:01:30Z",
        }),
        0,
      ),
    ).toBe("1m 30s");
    expect(
      buildDuration(
        build({ status: "running", started_at: "2026-03-24T00:00:00Z" }),
        Date.parse("2026-03-24T00:00:45Z"),
      ),
    ).toBe("45s");
  });
});
