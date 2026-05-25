import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { StepList } from "./StepList";
import type { BuildStep } from "../types";

function makeStep(overrides: Partial<BuildStep> = {}): BuildStep {
  return {
    id: "step-1",
    build_id: "build-1",
    step_index: 0,
    name: "verify",
    command: "echo ok",
    status: "pending",
    worker_id: null,
    started_at: null,
    finished_at: null,
    exit_code: null,
    stdout: null,
    stderr: null,
    error_message: null,
    ...overrides,
  };
}

describe("StepList", () => {
  it("renders step status, derived duration, and failure context in the timeline", () => {
    render(
      <StepList
        buildID="build-1"
        activeStepIndex={2}
        steps={[
          makeStep({
            step_index: 2,
            name: "deploy",
            status: "failed",
            started_at: "2026-04-01T00:00:00Z",
            finished_at: "2026-04-01T00:01:05Z",
            exit_code: 1,
            error_message: "command exited with status 1",
          }),
        ]}
      />,
    );

    expect(screen.getByText("Step 2 · Current step")).toBeTruthy();
    expect(screen.getByText("Duration 1m 5s")).toBeTruthy();
    expect(screen.getByText("command exited with status 1")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Open logs" })).toBeTruthy();
  });

  it("renders full command when short", () => {
    render(
      <StepList
        buildID="build-1"
        steps={[makeStep({ command: "echo hello" })]}
      />,
    );

    const command = screen.getByTitle("echo hello");
    expect(command.textContent).toBe("echo hello");
  });

  it("truncates long command and preserves full command in title", () => {
    const longCommand =
      "echo one && echo two && echo three && echo four && echo five && echo six && echo seven && echo eight";
    render(
      <StepList
        buildID="build-1"
        steps={[makeStep({ command: longCommand })]}
      />,
    );

    const command = screen.getByTitle(longCommand);
    expect(command.textContent?.endsWith("...")).toBe(true);
    expect(command.textContent?.length).toBe(72);
  });
});
